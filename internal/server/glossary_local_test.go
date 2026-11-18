package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"deadlocktranslator/internal/assets"
	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
	"deadlocktranslator/internal/translate"
)

func TestLocalGlossaryAdminRoutes(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	dir := t.TempDir()
	store, err := settings.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	book, err := translate.NewProjectLexicon(assets.Files, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := provider.NewClient()
	defer client.Close()
	engine := translate.New(client, book)
	local := translate.NewLocalGlossaries(assets.Files, dir, t.TempDir(), engine)
	server, err := New(store, engine, assets.Files, "test", nil, local)
	if err != nil {
		t.Fatal(err)
	}
	request := func(handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Host = "127.0.0.1:8791"
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Deadlock-Bridge", "1")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	admin := server.Handler(false)
	if got := request(admin, http.MethodGet, "/api/v1/glossaries?name=equipment", nil); got.Code != 200 {
		t.Fatalf("read status = %d: %s", got.Code, got.Body.String())
	}
	payload, _ := json.Marshal(map[string]string{"name": "custom", "content": "LimenGate = 界门\n"})
	if got := request(admin, http.MethodPost, "/api/v1/glossaries", payload); got.Code != 200 {
		t.Fatalf("save status = %d: %s", got.Code, got.Body.String())
	}
	if got := request(admin, http.MethodGet, "/api/v1/glossaries?name=custom", nil); got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte("LimenGate")) {
		t.Fatalf("read custom status = %d: %s", got.Code, got.Body.String())
	}
	if got := request(server.Handler(true), http.MethodGet, "/api/v1/glossaries?name=custom", nil); got.Code != 404 {
		t.Fatalf("game route status = %d", got.Code)
	}
}
