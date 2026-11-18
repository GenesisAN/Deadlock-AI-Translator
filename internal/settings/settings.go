package settings

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"deadlocktranslator/internal/provider"
)

const Schema = 2
const Mask = "********"

type Context struct {
	Prompt   string `json:"prompt"`
	Glossary string `json:"glossary"`
}
type Rule struct {
	Prefix    string `json:"prefix"`
	Direction string `json:"direction,omitempty"`
	Target    string `json:"targetLanguage,omitempty"`
	Prompt    string `json:"prompt"`
}
type Languages struct {
	Source string `json:"sourceLanguage"`
	Target string `json:"targetLanguage"`
}
type Game struct {
	Enabled            bool     `json:"enabled"`
	TargetLanguage     string   `json:"targetLanguage"`
	DisplayMode        string   `json:"displayMode"`
	Outgoing           string   `json:"outgoing"`
	OutgoingTarget     string   `json:"outgoingTarget"`
	OutgoingResumeMode string   `json:"outgoingResumeMode,omitempty"`
	Force              bool     `json:"force"`
	TranslateOwn       bool     `json:"translateOwn"`
	UILang             string   `json:"uiLang"`
	Timeout            int      `json:"timeoutMs"`
	Order              []string `json:"order,omitempty"`
	Revision           string   `json:"_bridgeRev,omitempty"`
}
type Config struct {
	Schema        int                          `json:"schemaVersion"`
	Port          int                          `json:"port"`
	Provider      string                       `json:"provider"`
	Providers     map[string]provider.Settings `json:"providers"`
	Fallback      []string                     `json:"fallbackProviders"`
	Context       Context                      `json:"translationContext"`
	Rules         []Rule                       `json:"prefixRules,omitempty"`
	Languages     Languages                    `json:"defaults"`
	Timeout       int                          `json:"timeoutMs"`
	MaxConcurrent int                          `json:"maxConcurrent"`
	Game          Game                         `json:"ui"`
	ChatLog       struct {
		Enabled bool   `json:"enabled"`
		Dir     string `json:"dir,omitempty"`
	} `json:"chatLog"`
	Credentials string `json:"credentialRef,omitempty"`
}

type storedKey struct {
	Key    string `json:"key"`
	Origin string `json:"origin"`
}

type Snapshot struct {
	Config   Config
	Keys     map[string]string
	Revision string
}

func defaults() Config {
	c := Config{Schema: Schema, Port: 8791, Provider: "deepseek", Providers: map[string]provider.Settings{}, Fallback: []string{}, Languages: Languages{"auto", "zh-Hans"}, Timeout: 15000, MaxConcurrent: 8, Game: Game{Enabled: true, TargetLanguage: "zh-Hans", DisplayMode: "bilingual", Outgoing: "translation", OutgoingTarget: "en", UILang: "zh", Timeout: 8000}}
	c.ChatLog.Enabled = true
	for _, p := range provider.List() {
		c.Providers[p.ID] = p.Default
	}
	return c
}

type Store struct {
	mu       sync.RWMutex
	dir      string
	lockFile *os.File
	current  Snapshot
}

func Open(dir, legacy string) (store *Store, resultErr error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, e := os.OpenFile(filepath.Join(dir, "bridge.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	defer func() {
		if resultErr != nil {
			_ = f.Close()
		}
	}()
	if e = lock(f); e != nil {
		return nil, errors.New("该配置目录已由另一个 Go 桥接进程使用")
	}
	s := &Store{dir: dir, lockFile: f}
	c := defaults()
	keys := map[string]string{}
	b, err := readFile(filepath.Join(dir, "settings.json"))
	if err == nil {
		if err = json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("settings.json: %w", err)
		}
		if c.Schema != Schema {
			return nil, fmt.Errorf("不支持的配置版本 %d", c.Schema)
		}
		for _, p := range provider.List() {
			if v, ok := c.Providers[p.ID]; ok {
				v.Model = p.ResolveModel(v)
				c.Providers[p.ID] = v
			}
		}
		if c.Credentials != "" {
			if !credentialID.MatchString(c.Credentials) {
				return nil, errors.New("无效的凭据引用")
			}
			content, e := readFile(filepath.Join(dir, "credentials-"+c.Credentials))
			if e != nil {
				return nil, e
			}
			content, e = unprotect(content)
			if e != nil {
				return nil, e
			}
			var persisted map[string]storedKey
			if e = json.Unmarshal(content, &persisted); e != nil {
				return nil, e
			}
			for id, record := range persisted {
				if p, ok := provider.Get(id); ok && record.Origin == provider.Origin(p, c.Providers[id]) {
					keys[id] = record.Key
				}
			}
		}
		if err = validate(c); err != nil {
			return nil, err
		}
		s.current = snapshot(c, keys)
	} else if errors.Is(err, os.ErrNotExist) {
		if legacy != "" {
			old, e := readFile(legacy)
			if e == nil {
				var patch map[string]json.RawMessage
				if e = json.Unmarshal(old, &patch); e != nil {
					return nil, fmt.Errorf("旧配置 %w", e)
				}
				if c, keys, e = apply(c, keys, patch, false); e != nil {
					return nil, fmt.Errorf("导入旧配置: %w", e)
				}
			} else if !errors.Is(e, os.ErrNotExist) {
				return nil, e
			}
		}
		if err = s.commit(c, keys); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.lockFile.Close() }

func readFile(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(b) > 1<<20 {
		return nil, errors.New("配置文件过大")
	}
	return bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}), e
}

func snapshot(c Config, keys map[string]string) Snapshot {
	encoded, _ := json.Marshal(c)
	h := sha256.Sum256(encoded)
	return Snapshot{c, keys, hex.EncodeToString(h[:8])}
}

// Published snapshots are immutable. Writers clone maps and replace them only after a successful save.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}
func (s *Store) Update(raw map[string]json.RawMessage, rev string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rev != "" && rev != s.current.Revision {
		return Snapshot{}, ErrConflict
	}
	c, keys, e := apply(s.current.Config, s.current.Keys, raw, true)
	if e != nil {
		return Snapshot{}, e
	}
	if c.Port != s.current.Config.Port {
		return Snapshot{}, errors.New("请在重启服务前修改 settings.json 中的 port")
	}
	if e = s.commit(c, keys); e != nil {
		return Snapshot{}, e
	}
	return s.current, nil
}

var ErrConflict = errors.New("设置已被其他窗口更新，请刷新后重试")

func (s *Store) Game() Game {
	snap := s.Snapshot()
	game := snap.Config.Game
	game.Revision = gameRevision(game)
	return game
}
func gameRevision(game Game) string {
	game.Revision = ""
	b, _ := json.Marshal(game)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}
func (s *Store) UpdateGame(g Game) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.current.Config
	if g.Revision != "" && g.Revision != gameRevision(c.Game) {
		return ErrConflict
	}
	if g.Revision == "" {
		return ErrConflict
	}
	g.Revision = ""
	c.Game = g
	c.Languages.Target = g.TargetLanguage
	if err := validate(c); err != nil {
		return err
	}
	if gameRevision(c.Game) == gameRevision(s.current.Config.Game) {
		return nil
	}
	return s.commit(c, s.current.Keys)
}

var credentialID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s *Store) commit(c Config, keys map[string]string) error {
	previous := s.current.Config.Credentials
	c.Credentials = previous
	newFile := ""
	if credentialsChanged(c, s.current, keys) && len(keys) > 0 {
		id := make([]byte, 16)
		if _, e := rand.Read(id); e != nil {
			return e
		}
		c.Credentials = hex.EncodeToString(id)
		newFile = filepath.Join(s.dir, "credentials-"+c.Credentials)
		records := make(map[string]storedKey, len(keys))
		for id, key := range keys {
			p, _ := provider.Get(id)
			records[id] = storedKey{key, provider.Origin(p, c.Providers[id])}
		}
		b, _ := json.Marshal(records)
		protected, e := protect(b)
		if e != nil {
			return e
		}
		if e = WriteAtomic(newFile, protected); e != nil {
			return e
		}
	}
	if len(keys) == 0 {
		c.Credentials = ""
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	b = append(b, '\n')
	err := WriteAtomic(filepath.Join(s.dir, "settings.json"), b)
	if err != nil {
		if newFile != "" {
			_ = os.Remove(newFile)
		}
		return err
	}
	s.current = snapshot(c, keys)
	if previous != "" && previous != c.Credentials {
		_ = os.Remove(filepath.Join(s.dir, "credentials-"+previous))
	}
	return nil
}
func credentialsChanged(c Config, previous Snapshot, keys map[string]string) bool {
	if len(keys) != len(previous.Keys) {
		return true
	}
	for id, key := range keys {
		p, _ := provider.Get(id)
		if key != previous.Keys[id] || provider.Origin(p, c.Providers[id]) != provider.Origin(p, previous.Config.Providers[id]) {
			return true
		}
	}
	return false
}

// WriteAtomic replaces a state file after the new contents have been written to disk.
func WriteAtomic(path string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".write-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = replace(f.Name(), path); e != nil {
		return e
	}
	syncDirectory(filepath.Dir(path))
	return nil
}

func apply(c Config, original map[string]string, raw map[string]json.RawMessage, patch bool) (Config, map[string]string, error) {
	merged := c
	merged.Providers = make(map[string]provider.Settings, len(c.Providers))
	for k, v := range c.Providers {
		merged.Providers[k] = v
	}
	merged.Fallback = append([]string{}, c.Fallback...)
	merged.Game.Order = append([]string(nil), c.Game.Order...)
	keys := map[string]string{}
	for k, v := range original {
		if v != "" {
			keys[k] = v
		}
	}
	merged.Rules = append([]Rule(nil), c.Rules...)
	for name, dst := range map[string]any{"port": &merged.Port, "provider": &merged.Provider, "fallbackProviders": &merged.Fallback, "defaults": &merged.Languages, "timeoutMs": &merged.Timeout, "maxConcurrent": &merged.MaxConcurrent, "ui": &merged.Game, "chatLog": &merged.ChatLog, "translationContext": &merged.Context, "prefixRules": &merged.Rules} {
		if value, ok := raw[name]; ok {
			if e := json.Unmarshal(value, dst); e != nil {
				return c, nil, fmt.Errorf("配置字段 %s 无效", name)
			}
		}
	}
	if value, ok := raw["translateOwn"]; ok {
		if e := json.Unmarshal(value, &merged.Game.TranslateOwn); e != nil {
			return c, nil, errors.New("无效的 translateOwn")
		}
	}
	if value, ok := raw["ui"]; ok {
		var ui map[string]json.RawMessage
		_ = json.Unmarshal(value, &ui)
		if _, ownsTarget := ui["targetLanguage"]; ownsTarget {
			merged.Languages.Target = merged.Game.TargetLanguage
		} else {
			merged.Game.TargetLanguage = merged.Languages.Target
		}
	} else {
		merged.Game.TargetLanguage = merged.Languages.Target
	}
	merged.Game.Revision = ""
	merged.Context.Prompt = strings.TrimSpace(merged.Context.Prompt)
	for _, p := range provider.List() {
		var v struct {
			provider.Settings
			Key *string `json:"apiKey"`
		}
		v.Settings = merged.Providers[p.ID]
		if data, ok := raw[p.ID]; ok {
			if e := json.Unmarshal(data, &v); e != nil {
				return c, nil, fmt.Errorf("服务商 %s 配置无效", p.ID)
			}
		}
		if p.ID == merged.Provider && p.AcceptsKey {
			if data, ok := raw["apiKey"]; ok {
				if e := json.Unmarshal(data, &v.Key); e != nil {
					return c, nil, errors.New("无效的 API Key")
				}
			}
		}
		if !p.EditableBaseURL {
			v.BaseURL = p.Default.BaseURL
		}
		if p.ID != "microsoft" && p.ID != "deepl" {
			v.Endpoint = p.Default.Endpoint
		}
		v.BaseURL = strings.TrimSpace(v.BaseURL)
		v.Endpoint = strings.TrimSpace(v.Endpoint)
		v.Model = p.ResolveModel(v.Settings)
		oldOrigin := provider.Origin(p, c.Providers[p.ID])
		newOrigin := provider.Origin(p, v.Settings)
		if patch && oldOrigin != newOrigin {
			delete(keys, p.ID)
		}
		if p.AcceptsKey && v.Key != nil && *v.Key != Mask {
			key := strings.TrimSpace(*v.Key)
			if len(key) > 8192 || strings.ContainsAny(key, "\r\n") {
				return c, nil, errors.New("无效的 API Key")
			}
			if key == "" {
				delete(keys, p.ID)
			} else {
				keys[p.ID] = key
			}
		}
		merged.Providers[p.ID] = v.Settings
	}
	if merged.Fallback == nil {
		merged.Fallback = []string{}
	}
	if e := validate(merged); e != nil {
		return c, nil, e
	}
	return merged, keys, nil
}

var language = regexp.MustCompile(`(?i)^[a-z]{2,3}(-[a-z0-9]{2,8}){0,2}$`)
var reserved = regexp.MustCompile(`(?i)LCTPH\s*\d`)

func Language(s string) bool { return language.MatchString(s) }
func Size(s string) int      { return utf8.RuneCountInString(s) }
func ValidateContext(c Context) error {
	if Size(c.Prompt) > 1500 || Size(c.Glossary) > 5000 {
		return errors.New("前置提示词或词库超过长度限制")
	}
	seen := map[string]bool{}
	for i, line := range strings.Split(c.Glossary, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a, b, ok := strings.Cut(line, "=")
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
		normalized := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(a, "‘", "'"), "’", "'")), " "))
		if !ok || a == "" || b == "" || strings.Contains(b, "=") || Size(a) > 80 || Size(b) > 80 || reserved.MatchString(line) || seen[normalized] {
			return fmt.Errorf("词库第 %d 行应使用唯一的 English = 简体中文，每侧最多 80 字", i+1)
		}
		seen[normalized] = true
	}
	return nil
}
func validate(c Config) error {
	if c.Port < 1024 || c.Port > 65535 || c.Port == 8792 || c.Timeout < 100 || c.Timeout > 30000 || c.MaxConcurrent < 2 || c.MaxConcurrent > 32 {
		return errors.New("端口、超时或并发数超出范围")
	}
	if (c.Languages.Source != "auto" && !Language(c.Languages.Source)) || !Language(c.Languages.Target) || !Language(c.Game.TargetLanguage) || !Language(c.Game.OutgoingTarget) {
		return errors.New("无效的语言代码")
	}
	if c.Game.Outgoing != "off" && c.Game.Outgoing != "translation" && c.Game.Outgoing != "bilingual" {
		return errors.New("无效的发出模式")
	}
	if c.Game.Timeout < 100 || c.Game.Timeout > 30000 || len(c.Game.Order) > 16 {
		return errors.New("无效的游戏设置")
	}
	if _, ok := provider.Get(c.Provider); !ok {
		return errors.New("未知翻译服务商")
	}
	if len(c.Rules) > 24 {
		return errors.New("前缀规则最多 24 条")
	}
	for _, r := range c.Rules {
		if strings.TrimSpace(r.Prefix) == "" || Size(r.Prefix) > 80 || strings.TrimSpace(r.Prompt) == "" || Size(r.Prompt) > 500 || (r.Direction != "" && r.Direction != "incoming" && r.Direction != "outgoing") || (r.Target != "" && !Language(r.Target)) {
			return errors.New("无效的前缀规则")
		}
	}
	if len(c.Fallback) > 12 {
		return errors.New("备用服务商过多")
	}
	for _, id := range c.Fallback {
		if _, ok := provider.Get(id); !ok {
			return fmt.Errorf("未知备用服务商 %q", id)
		}
	}
	for _, p := range provider.List() {
		s := c.Providers[p.ID]
		if len(s.Model) > 256 || len(s.Region) > 80 || strings.ContainsAny(s.Region, "\r\n") {
			return errors.New("无效的模型或区域")
		}
		for _, u := range []string{s.BaseURL, s.Endpoint} {
			if u != "" {
				if _, e := provider.ParseEndpoint(u); e != nil {
					return e
				}
			}
		}
	}
	return ValidateContext(c.Context)
}

func (snap Snapshot) Public() map[string]any {
	c := snap.Config
	public := map[string]any{"port": c.Port, "provider": c.Provider, "fallbackProviders": c.Fallback, "translationContext": c.Context, "prefixRules": c.Rules, "defaults": c.Languages, "timeoutMs": c.Timeout, "maxConcurrent": c.MaxConcurrent, "ui": c.Game, "translateOwn": c.Game.TranslateOwn, "chatLog": c.ChatLog}
	for _, p := range provider.List() {
		v := c.Providers[p.ID]
		key := ""
		if snap.Keys[p.ID] != "" {
			key = Mask
		}
		public[p.ID] = map[string]any{"baseUrl": v.BaseURL, "endpoint": v.Endpoint, "model": v.Model, "region": v.Region, "apiKey": key, "hasApiKey": key != ""}
	}
	return public
}
