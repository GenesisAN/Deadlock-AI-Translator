package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"deadlocktranslator/internal/provider"
)

func TestProviderModelsFromSavedSettingsAndUpdates(t *testing.T) {
	dir := t.TempDir()
	cfg := defaults()
	cfg.Providers["deepseek"] = provider.Settings{Model: "deepseek-v4-flash"}
	cfg.Providers["openai"] = provider.Settings{BaseURL: "https://api.openai.com/v1/", Model: "gpt-4o-mini"}
	cfg.Providers["openrouter"] = provider.Settings{Model: "openai/gpt-4o-mini"}
	cfg.Providers["qwen"] = provider.Settings{Model: "qwen-plus"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	initial := store.Snapshot()
	checkModels := func(snap Snapshot, want map[string]string) {
		t.Helper()
		for id, model := range want {
			if got := snap.Config.Providers[id].Model; got != model {
				t.Errorf("%s model = %q, want %q", id, got, model)
			}
		}
	}
	checkModels(initial, map[string]string{
		"deepseek": "deepseek-flash", "openai": "gpt-6-luna", "openrouter": "openai/gpt-6-luna", "qwen": "qwen-plus",
	})

	updated, err := store.Update(map[string]json.RawMessage{
		"deepseek": json.RawMessage(`{"model":" deepseek-v4-pro "}`),
		"openai":   json.RawMessage(`{"baseUrl":"https://example.com/v1","model":"gpt-4o-mini"}`),
		"qwen":     json.RawMessage(`{"model":""}`),
	}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	checkModels(updated, map[string]string{
		"deepseek": "deepseek-v4-pro", "openai": "gpt-4o-mini", "qwen": "qwen3.8-flash",
	})
}
