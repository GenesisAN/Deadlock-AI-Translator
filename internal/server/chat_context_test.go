package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"deadlocktranslator/internal/assets"
	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
	"deadlocktranslator/internal/translate"
)

type contextTranslator struct{ received chan provider.Input }

func (m contextTranslator) Translate(_ context.Context, _ provider.Preset, _ provider.Settings, _ string, in provider.Input) (provider.Output, error) {
	m.received <- in
	return provider.Output{Translation: "the pig is here"}, nil
}

func TestGameTranslationForwardsRecentChat(t *testing.T) {
	stateDir := t.TempDir()
	store, err := settings.Open(stateDir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	choice, _ := json.Marshal("ollama")
	if _, err := store.Update(map[string]json.RawMessage{"provider": choice}, store.Snapshot().Revision); err != nil {
		t.Fatal(err)
	}
	book, err := translate.NewProjectLexicon(assets.Files, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	model := contextTranslator{received: make(chan provider.Input, 1)}
	engine := translate.New(model, book)
	engine.SetSampleRecorder(translate.NewSampleRecorder(stateDir))
	s, err := New(store, engine, assets.Files, "test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://127.0.0.1:8792/api/v1/community-translate", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Deadlock-Bridge", "1")
		w := httptest.NewRecorder()
		s.Handler(true).ServeHTTP(w, r)
		return w
	}
	valid := `{"sourceLanguage":"zh-Hans","targetLanguage":"en","mode":"incoming","items":[{"i":1,"t":"猪来了"}],"history":[{"direction":"outgoing","text":"我去中路"}]}`
	if result := request(valid); result.Code != 200 {
		t.Fatalf("translation status = %d: %s", result.Code, result.Body.String())
	}
	if got := <-model.received; len(got.History) != 1 || got.History[0].Direction != "outgoing" || got.History[0].Text != "我去中路" {
		t.Fatalf("history sent to AI = %+v", got.History)
	}
	paths, err := filepath.Glob(filepath.Join(stateDir, "translation-samples", "*.jsonl"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("game sample files = %v, %v", paths, err)
	}
	if data, err := os.ReadFile(paths[0]); err != nil || !bytes.Contains(data, []byte(`"original":"猪来了"`)) {
		t.Fatalf("game sample missing: %s (%v)", data, err)
	}
	invalid := `{"targetLanguage":"en","items":[{"i":2,"t":"猪来了"}],"history":[{"direction":"outgoing","text":""}]}`
	if result := request(invalid); result.Code != 400 {
		t.Fatalf("invalid history status = %d: %s", result.Code, result.Body.String())
	}
}
