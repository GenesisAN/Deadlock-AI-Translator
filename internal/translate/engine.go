package translate

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

type Translator interface {
	Translate(context.Context, provider.Preset, provider.Settings, string, provider.Input) (provider.Output, error)
}
type Request struct {
	Text     string `json:"text"`
	Source   string `json:"sourceLanguage"`
	Target   string `json:"targetLanguage"`
	Timeout  int    `json:"timeoutMs"`
	Outgoing bool   `json:"outgoing"`
}
type Detail struct {
	Method    string             `json:"method"`
	Provider  string             `json:"provider,omitempty"`
	Text      string             `json:"text,omitempty"`
	Hints     []string           `json:"hints"`
	Protected []protected        `json:"protectedTerms"`
	Messages  []provider.Message `json:"messages,omitempty"`
}
type call struct {
	done      chan struct{}
	cancel    context.CancelFunc
	listeners int
	result    provider.Output
	err       error
}
type entry struct {
	key   string
	value provider.Output
	until time.Time
}

type Engine struct {
	network          Translator
	base             *Lexicon
	mu               sync.Mutex
	glossary         string
	current          *Lexicon
	queue            map[string]*call
	lru              *list.List
	cache            map[string]*list.Element
	active, incoming int
}

func New(network Translator, book *Lexicon) *Engine {
	return &Engine{network: network, base: book, current: book, queue: map[string]*call{}, lru: list.New(), cache: map[string]*list.Element{}}
}

func (e *Engine) lexicon(gloss string, preview bool) *Lexicon {
	if preview {
		return e.base.withGlossary(gloss)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.glossary != gloss {
		e.current = e.base.withGlossary(gloss)
		e.glossary = gloss
	}
	return e.current
}

func (e *Engine) Run(parent context.Context, snap settings.Snapshot, req Request, override *settings.Context, dry, test bool) (provider.Output, *Detail, error) {
	var out provider.Output
	c := snap.Config
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || settings.Size(req.Text) > 4000 {
		return out, nil, ErrInput
	}
	if req.Source == "" {
		req.Source = c.Languages.Source
	}
	if req.Target == "" {
		req.Target = c.Languages.Target
	}
	if (req.Source != "auto" && !settings.Language(req.Source)) || !settings.Language(req.Target) {
		return out, nil, ErrInput
	}
	timeout := c.Timeout
	if req.Timeout > 0 && req.Timeout < timeout {
		timeout = req.Timeout
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	translationContext := c.Context
	if override != nil {
		translationContext = *override
		if err := settings.ValidateContext(translationContext); err != nil {
			return out, nil, fmt.Errorf("%w: %s", ErrInput, err)
		}
	}
	book := e.lexicon(translationContext.Glossary, override != nil)
	if !test {
		if hit := book.lookup(req.Text, req.Source, req.Target); hit != "" {
			return provider.Output{Translation: hit, Provider: "dictionary"}, &Detail{Method: "dictionary"}, nil
		}
	}
	ids := []string{c.Provider}
	if !test {
		ids = append(ids, c.Fallback...)
	}
	prompt := translationContext.Prompt
	normalized := normalize(req.Text)
	direction := "incoming"
	if req.Outgoing {
		direction = "outgoing"
	}
	for _, r := range c.Rules {
		prefix := normalize(r.Prefix)
		if (r.Direction == "" || r.Direction == direction) && (r.Target == "" || strings.EqualFold(r.Target, req.Target)) &&
			strings.HasPrefix(normalized, prefix) && boundary(normalized, prefix, 0) {
			prompt = strings.TrimSpace(prompt + "\n" + r.Prompt)
			break
		}
	}
	prepare := func(p provider.Preset) (provider.Input, *Detail, preparedText) {
		prepared := preparedText{Text: req.Text, Hints: []string{}, Protected: []protected{}}
		if !test {
			prepared = book.prepare(req.Text, req.Target, p.AI)
		}
		in := provider.Input{
			Text: prepared.Text, Source: req.Source, Target: req.Target,
			Prompt: prompt, Hints: prepared.Hints, HasPlaceholders: len(prepared.Protected) > 0,
		}
		d := &Detail{Method: "provider", Provider: p.ID, Text: in.Text, Hints: prepared.Hints, Protected: prepared.Protected}
		if p.AI && override != nil {
			d.Messages = provider.Messages(in)
		}
		return in, d, prepared
	}
	if dry {
		p, _ := provider.Get(c.Provider)
		_, detail, _ := prepare(p)
		return out, detail, nil
	}
	var detail *Detail
	work := func(workCtx context.Context) (provider.Output, error) {
		used := map[string]bool{}
		var messages []string
		for i, id := range ids {
			if used[id] {
				continue
			}
			used[id] = true
			p, _ := provider.Get(id)
			if p.RequiresKey && snap.Keys[id] == "" {
				if override != nil {
					_, detail, _ = prepare(p)
				}
				messages = append(messages, id+": 请先配置 API Key")
				continue
			}
			if err := workCtx.Err(); err != nil {
				return out, err
			}
			in, d, prepared := prepare(p)
			if override != nil {
				detail = d
			}
			deadline, _ := workCtx.Deadline()
			budget := time.Until(deadline)
			if i < len(ids)-1 {
				budget = budget * 65 / 100
			}
			attempt, stop := context.WithTimeout(workCtx, budget)
			result, err := e.network.Translate(attempt, p, c.Providers[id], snap.Keys[id], in)
			stop()
			if err == nil {
				result.Translation, err = prepared.restore(result.Translation)
			}
			if err == nil {
				result.Fallback = i > 0
				return result, nil
			}
			messages = append(messages, id+": "+err.Error())
		}
		if err := workCtx.Err(); err != nil {
			return out, err
		}
		return out, errors.New(strings.Join(messages, " | "))
	}
	if test || override != nil {
		if !e.acquire(c.MaxConcurrent, false) {
			return out, nil, ErrBusy
		}
		defer e.release(false)
		result, err := work(ctx)
		return result, detail, err
	}
	raw, _ := json.Marshal([]any{snap.Revision, req.Text, req.Source, req.Target, req.Outgoing})
	key := string(raw)
	e.mu.Lock()
	if el := e.cache[key]; el != nil {
		v := el.Value.(entry)
		if time.Now().Before(v.until) {
			e.lru.MoveToFront(el)
			e.mu.Unlock()
			v.value.Cached = true
			return v.value, nil, nil
		}
		delete(e.cache, key)
		e.lru.Remove(el)
	}
	requestKey := fmt.Sprintf("%s/%d", key, timeout)
	pending := e.queue[requestKey]
	if pending == nil {
		if !e.acquireLocked(c.MaxConcurrent, req.Outgoing) {
			e.mu.Unlock()
			return out, nil, ErrBusy
		}
		workCtx, abort := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Millisecond)
		pending = &call{done: make(chan struct{}), cancel: abort}
		e.queue[requestKey] = pending
		go func(p *call) {
			result, err := work(workCtx)
			abort()
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.queue[requestKey] == p {
				delete(e.queue, requestKey)
				if err == nil && !result.Fallback {
					e.remember(key, result)
				}
			}
			p.result, p.err = result, err
			e.releaseLocked(req.Outgoing)
			close(p.done)
		}(pending)
	}
	pending.listeners++
	e.mu.Unlock()
	select {
	case <-pending.done:
		return pending.result, nil, pending.err
	case <-ctx.Done():
		e.mu.Lock()
		pending.listeners--
		if pending.listeners == 0 && e.queue[requestKey] == pending {
			delete(e.queue, requestKey)
			pending.cancel()
		}
		e.mu.Unlock()
		return out, nil, ctx.Err()
	}
}

var ErrInput = errors.New("文本或语言设置无效")
var ErrBusy = errors.New("翻译服务繁忙")

func (e *Engine) acquire(limit int, outgoing bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.acquireLocked(limit, outgoing)
}
func (e *Engine) acquireLocked(limit int, outgoing bool) bool {
	if e.active >= limit || (!outgoing && e.incoming >= limit-1) {
		return false
	}
	e.active++
	if !outgoing {
		e.incoming++
	}
	return true
}
func (e *Engine) release(outgoing bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releaseLocked(outgoing)
}
func (e *Engine) releaseLocked(outgoing bool) {
	e.active--
	if !outgoing {
		e.incoming--
	}
}
func (e *Engine) remember(k string, v provider.Output) {
	if el := e.cache[k]; el != nil {
		el.Value = entry{k, v, time.Now().Add(5 * time.Minute)}
		e.lru.MoveToFront(el)
		return
	}
	e.cache[k] = e.lru.PushFront(entry{k, v, time.Now().Add(5 * time.Minute)})
	if e.lru.Len() > 512 {
		last := e.lru.Back()
		delete(e.cache, last.Value.(entry).key)
		e.lru.Remove(last)
	}
}
