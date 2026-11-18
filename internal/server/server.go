package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
	"deadlocktranslator/internal/translate"
)

type Server struct {
	store           *settings.Store
	engine          *translate.Engine
	updates         *Updates
	localGlossaries *translate.LocalGlossaries
	ui, bridge      []byte
	version         string
	started         int64
	tls             atomic.Bool
	nav, fetch      atomic.Int64
}

func New(store *settings.Store, engine *translate.Engine, assets fs.FS, version string, updates *Updates, localGlossaries *translate.LocalGlossaries) (*Server, error) {
	ui, e := fs.ReadFile(assets, "web/settings.html")
	if e != nil {
		return nil, e
	}
	bridge, e := fs.ReadFile(assets, "web/game-bridge.html")
	if e != nil {
		return nil, e
	}
	ui = bytes.Replace(ui, []byte("<html"), []byte("<html data-native-bridge"), 1)
	bridge = bytes.Replace(bridge, []byte("<html"), []byte("<html data-native-bridge"), 1)
	return &Server{store: store, engine: engine, ui: ui, bridge: bridge, version: version, started: time.Now().UnixMilli(), updates: updates, localGlossaries: localGlossaries}, nil
}
func (s *Server) TLSReady(ready bool) { s.tls.Store(ready) }

func (s *Server) Handler(game bool) http.Handler {
	routes := http.NewServeMux()
	if game {
		routes.HandleFunc("GET /community-bridge", func(w http.ResponseWriter, r *http.Request) {
			s.nav.Store(time.Now().UnixMilli())
			page(w, s.bridge)
		})
		routes.HandleFunc("POST /api/v1/game-state", s.gameState)
		routes.HandleFunc("POST /api/v1/community-translate", s.community)
		routes.HandleFunc("POST /api/v1/game-chat-log", s.gameChatLog)
	} else {
		routes.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) { page(w, s.ui) })
		routes.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ui", http.StatusFound) })
		routes.HandleFunc("GET /api/v1/config", s.getConfig)
		routes.HandleFunc("POST /api/v1/config", s.save)
		routes.HandleFunc("GET /api/v1/health", s.health)
		routes.HandleFunc("POST /api/v1/update/check", s.update)
		routes.HandleFunc("POST /api/v1/update/install", s.update)
		routes.HandleFunc("GET /api/v1/glossaries", s.readGlossary)
		routes.HandleFunc("POST /api/v1/glossaries", s.saveGlossary)
		routes.HandleFunc("POST /api/v1/translate", s.run)
		routes.HandleFunc("POST /api/v1/test", s.run)
		routes.HandleFunc("POST /api/v1/translation-preview", s.preview)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		host, port, err := net.SplitHostPort(r.Host)
		expected := r.Context().Value(http.LocalAddrContextKey)
		loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
		if err != nil || !loopback || (expected != nil && port != strconv.Itoa(expected.(*net.TCPAddr).Port)) {
			fail(w, http.StatusForbidden, "无效的本机 Host")
			return
		}
		scheme := "http"
		if game {
			scheme = "https"
		}
		origin := r.Header.Get("Origin")
		if (origin != "" && origin != scheme+"://"+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" ||
			(r.Method == http.MethodPost && r.Header.Get("X-Deadlock-Bridge") != "1") {
			fail(w, http.StatusForbidden, "仅允许同源请求")
			return
		}
		routes.ServeHTTP(w, r)
	})
}
func (s *Server) readGlossary(w http.ResponseWriter, r *http.Request) {
	if s.localGlossaries == nil {
		fail(w, 404, "词库管理不可用")
		return
	}
	content, err := s.localGlossaries.Read(r.URL.Query().Get("name"))
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	send(w, 200, map[string]any{"ok": true, "content": content})
}
func (s *Server) saveGlossary(w http.ResponseWriter, r *http.Request) {
	if s.localGlossaries == nil {
		fail(w, 404, "词库管理不可用")
		return
	}
	var request struct{ Name, Content string }
	if !decodeLimit(w, r, &request, 3<<20) {
		return
	}
	if err := s.localGlossaries.Save(request.Name, request.Content); err != nil {
		fail(w, 400, err.Error())
		return
	}
	send(w, 200, map[string]any{"ok": true})
}

func page(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; form-action 'none'; base-uri 'none'")
	_, _ = w.Write(b)
}
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, reason string) {
	send(w, status, map[string]any{"ok": false, "error": reason})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	return decodeLimit(w, r, out, 64<<10)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		fail(w, 415, "需要 JSON 请求")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if e := d.Decode(out); e != nil || d.Decode(new(any)) != io.EOF {
		fail(w, 400, "无效的 JSON 请求")
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UnixMilli()
	nav, fetch := s.nav.Load(), s.fetch.Load()
	send(w, 200, map[string]any{
		"ok": true, "version": s.version, "updatesEnabled": s.updates != nil,
		"glossaryVersion": s.engine.GlossaryVersion(),
		"providers":       provider.List(), "gameRunning": nav != 0 && now-max(nav, fetch) < 60000, "tlsReady": s.tls.Load(),
		"channels": map[string]any{"now": now, "startedAt": s.started, "byChannel": map[string]int64{"bridgeNav": nav, "gameFetch": fetch}},
	})
}
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Snapshot()
	send(w, 200, map[string]any{"ok": true, "config": snap.Public(), "rev": snap.Revision})
}
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Config map[string]json.RawMessage
		Rev    string
	}
	if !decode(w, r, &v) {
		return
	}
	if v.Config == nil {
		fail(w, 400, "缺少配置")
		return
	}
	snap, e := s.store.Update(v.Config, v.Rev)
	if errors.Is(e, settings.ErrConflict) {
		fail(w, 409, e.Error())
		return
	}
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	send(w, 200, map[string]any{"ok": true, "config": snap.Public(), "rev": snap.Revision})
}
func (s *Server) gameState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op    string
		Value *settings.Game
	}
	if !decode(w, r, &req) {
		return
	}
	s.fetch.Store(time.Now().UnixMilli())
	switch req.Op {
	case "get":
		send(w, 200, map[string]any{
			"ok": true, "val": s.store.Game(), "chatLog": s.store.Snapshot().Config.ChatLog.Enabled,
			"glossaryVersion": s.engine.GlossaryVersion(),
		})
	case "put":
		if req.Value == nil {
			fail(w, 400, "缺少游戏设置")
			return
		}
		err := s.store.UpdateGame(*req.Value)
		if errors.Is(err, settings.ErrConflict) {
			send(w, 200, map[string]any{"ok": true, "val": s.store.Game(), "conflict": true})
			return
		}
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		send(w, 200, map[string]any{"ok": true, "val": s.store.Game()})
	default:
		fail(w, 400, "无效的操作")
	}
}

func (s *Server) gameChatLog(w http.ResponseWriter, r *http.Request) {
	var event struct {
		Direction   string
		Original    string
		Translation string
		Sent        string
		Sender      string
		Status      string
	}
	if !decode(w, r, &event) {
		return
	}
	var direction, status string
	switch event.Direction {
	case "incoming":
		direction = "[聊天][收]"
	case "outgoing":
		direction = "[聊天][发]"
	}
	switch event.Status {
	case "translated":
		status = "已翻译"
	case "untranslated":
		status = "未翻译"
	case "raw":
		status = "直发原文"
	case "fallback":
		status = "翻译未返回，使用原文"
	}
	if direction == "" || status == "" || strings.TrimSpace(event.Original) == "" {
		fail(w, 400, "bad_chat_event")
		return
	}
	s.fetch.Store(time.Now().UnixMilli())
	if s.store.Snapshot().Config.ChatLog.Enabled {
		attrs := []slog.Attr{slog.String("状态", status), slog.String("原文", event.Original)}
		if event.Sender != "" {
			attrs = append(attrs, slog.String("玩家", event.Sender))
		}
		if event.Translation != "" {
			attrs = append(attrs, slog.String("译文", event.Translation))
		}
		if event.Sent != "" {
			attrs = append(attrs, slog.String("发送", event.Sent))
		}
		slog.LogAttrs(r.Context(), slog.LevelInfo, direction, attrs...)
	}
	send(w, 200, map[string]any{"ok": true})
}

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	var req translate.Request
	if !decode(w, r, &req) {
		return
	}
	result, _, e := s.engine.Run(r.Context(), s.store.Snapshot(), req, nil, false, r.URL.Path == "/api/v1/test")
	respond(w, result, nil, e)
}
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		translate.Request
		settings.Context
		Dry bool `json:"dryRun"`
	}
	if !decode(w, r, &req) {
		return
	}
	result, detail, e := s.engine.Run(r.Context(), s.store.Snapshot(), req.Request, &req.Context, req.Dry, false)
	respond(w, result, detail, e)
}
func respond(w http.ResponseWriter, out provider.Output, detail *translate.Detail, e error) {
	status := 200
	data := struct {
		OK bool `json:"ok"`
		provider.Output
		Detail *translate.Detail `json:"details,omitempty"`
		Error  string            `json:"error,omitempty"`
	}{OK: e == nil, Output: out, Detail: detail}
	if e != nil {
		status = 502
		switch {
		case errors.Is(e, translate.ErrInput):
			status = 400
		case errors.Is(e, translate.ErrBusy):
			status = 503
		case errors.Is(e, context.DeadlineExceeded):
			status = 504
		}
		data.Error = e.Error()
	}
	send(w, status, data)
}

type batchItem struct {
	ID   int64  `json:"i"`
	Text string `json:"t"`
}

func (s *Server) community(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items   []batchItem
		Source  string `json:"sourceLanguage"`
		Target  string `json:"targetLanguage"`
		Mode    string
		History []provider.ChatMessage `json:"history"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 8 || !settings.Language(req.Target) ||
		(req.Source != "" && req.Source != "auto" && !settings.Language(req.Source)) {
		fail(w, 400, "bad_translation_request")
		return
	}
	n := 0
	ids := map[int64]bool{}
	for _, item := range req.Items {
		n += len(utf16.Encode([]rune(item.Text)))
		if strings.TrimSpace(item.Text) == "" || ids[item.ID] || item.ID > 9007199254740991 || item.ID < -9007199254740991 {
			fail(w, 400, "bad_translation_request")
			return
		}
		ids[item.ID] = true
	}
	if n > 2100 {
		fail(w, 400, "bad_translation_request")
		return
	}
	if len(req.History) > 10 {
		fail(w, 400, "bad_chat_context")
		return
	}
	historySize := 0
	for _, message := range req.History {
		historySize += settings.Size(message.Text)
		if (message.Direction != "incoming" && message.Direction != "outgoing") ||
			strings.TrimSpace(message.Text) == "" || settings.Size(message.Text) > 400 || historySize > 1000 {
			fail(w, 400, "bad_chat_context")
			return
		}
	}
	s.fetch.Store(time.Now().UnixMilli())
	snap := s.store.Snapshot()
	type outcome struct {
		result provider.Output
		err    error
	}
	results := make([]outcome, len(req.Items))
	var wg sync.WaitGroup
	for i, item := range req.Items {
		wg.Go(func() {
			out, _, err := s.engine.Run(r.Context(), snap, translate.Request{Text: item.Text, Source: req.Source, Target: req.Target, Timeout: 6200, Outgoing: req.Mode == "outgoing", History: req.History, Game: true}, nil, false, false)
			results[i] = outcome{out, err}
		})
	}
	wg.Wait()
	items := make([]batchItem, 0, len(results))
	prov := ""
	allCached := true
	for i, result := range results {
		if result.err != nil {
			continue
		}
		items = append(items, batchItem{req.Items[i].ID, result.result.Translation})
		if prov == "" {
			prov = result.result.Provider
		}
		allCached = allCached && result.result.Cached
	}
	if len(items) == 0 {
		fail(w, 200, results[0].err.Error())
		return
	}
	send(w, 200, map[string]any{"ok": true, "items": items, "provider": prov, "cached": allCached})
}
