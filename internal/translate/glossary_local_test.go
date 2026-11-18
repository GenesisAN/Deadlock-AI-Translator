package translate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"deadlocktranslator/internal/assets"
	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

func TestLocalGlossariesSaveAndReload(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	dir := t.TempDir()
	book, err := NewProjectLexicon(assets.Files, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := book.lookup("Wraith", "en", "zh"); got != "灵魅" {
		t.Fatalf("bundled hero lookup changed: %q", got)
	}
	engine := New(translatorFunc(func(in provider.Input) provider.Output {
		return provider.Output{Translation: in.Text}
	}), book)
	local := NewLocalGlossaries(assets.Files, dir, t.TempDir(), engine)
	if err := local.Save("custom", "LimenGate = 界门\n"); err != nil {
		t.Fatal(err)
	}
	if got := engine.GlossaryVersion(); got != book.version+1 {
		t.Fatalf("glossary version after custom save = %d", got)
	}
	if got := engine.lexicon("", false).lookup("LimenGate", "en", "zh"); got != "界门" {
		t.Fatalf("custom lookup = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, customGlossaryName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("custom glossary unexpectedly wrote settings.json: %v", err)
	}

	content, err := local.Read("heroes")
	if err != nil {
		t.Fatal(err)
	}
	var heroes categoryGlossary
	if err := json.Unmarshal([]byte(content), &heroes); err != nil {
		t.Fatal(err)
	}
	for i := range heroes.Entries {
		if heroes.Entries[i].En == "Wraith" {
			heroes.Entries[i].Zh = "本机灵魅"
		}
	}
	data, _ := json.Marshal(heroes)
	if err := local.Save("heroes", string(data)); err != nil {
		t.Fatal(err)
	}
	if got := engine.GlossaryVersion(); got != book.version+2 {
		t.Fatalf("glossary version after hero save = %d", got)
	}
	if got := engine.lexicon("", false).lookup("Wraith", "en", "zh"); got != "本机灵魅" {
		t.Fatalf("edited project glossary lookup = %q", got)
	}
	restarted, err := NewProjectLexicon(assets.Files, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.lookup("LimenGate", "en", "zh"); got != "界门" {
		t.Fatalf("custom after restart = %q", got)
	}
	if got := restarted.lookup("Wraith", "en", "zh"); got != "本机灵魅" {
		t.Fatalf("heroes after restart = %q", got)
	}
	if err := local.Save("heroes", "{}"); err == nil {
		t.Fatal("empty project glossary accepted")
	}
	if got := engine.lexicon("", false).lookup("Wraith", "en", "zh"); got != "本机灵魅" {
		t.Fatalf("invalid save replaced active glossary: %q", got)
	}
	if err := local.Save("custom", "LimenGate = 界门\nlimengate = 重复\n"); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("duplicate custom entry accepted: %v", err)
	}
}

func TestLocalGlossariesMigrateSettings(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	dir := t.TempDir()
	store, err := settings.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	context, _ := json.Marshal(settings.Context{Prompt: "Keep tone", Glossary: "LimenGate = 界门"})
	if _, err := store.Update(map[string]json.RawMessage{"translationContext": context}, ""); err != nil {
		t.Fatal(err)
	}
	book, err := NewProjectLexicon(assets.Files, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := New(translatorFunc(func(in provider.Input) provider.Output { return provider.Output{Translation: in.Text} }), book)
	local := NewLocalGlossaries(assets.Files, dir, t.TempDir(), engine)
	if err := local.MigrateContext(store); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().Config.Context; got.Prompt != "Keep tone" || got.Glossary != "" {
		t.Fatalf("settings after migration = %+v", got)
	}
	if got := engine.lexicon("", false).lookup("LimenGate", "en", "zh"); got != "界门" {
		t.Fatalf("migrated entry = %q", got)
	}
	if err := local.MigrateContext(store); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
}

func TestProjectGlossaryChangesMergeNewEntries(t *testing.T) {
	base := map[string]any{"user": map[string]any{"zh": map[string]any{"old": "旧译", "remove": "删除"}}}
	edited := map[string]any{"user": map[string]any{"zh": map[string]any{"old": "新译"}}}
	patch := glossaryDiff(base, edited)
	future := map[string]any{"user": map[string]any{"zh": map[string]any{"old": "旧译", "remove": "删除", "added": "新版新增"}}}
	mergeGlossary(future, patch)
	want := map[string]any{"user": map[string]any{"zh": map[string]any{"old": "新译", "added": "新版新增"}}}
	if !reflect.DeepEqual(future, want) {
		t.Fatalf("merged glossary = %#v", future)
	}
	baseRows := map[string]any{"heroes": []any{
		map[string]any{"en": "Wraith", "zh": "灵魅"},
	}}
	editedRows := map[string]any{"heroes": []any{
		map[string]any{"en": "Wraith", "zh": "本机灵魅"},
	}}
	patchBytes, err := json.Marshal(glossaryDiff(baseRows, editedRows))
	if err != nil {
		t.Fatal(err)
	}
	var arrayPatch map[string]any
	if err := json.Unmarshal(patchBytes, &arrayPatch); err != nil {
		t.Fatal(err)
	}
	futureRows := map[string]any{"heroes": []any{
		map[string]any{"en": "Wraith", "zh": "灵魅"},
		map[string]any{"en": "New Hero", "zh": "新版英雄"},
	}}
	mergeGlossary(futureRows, arrayPatch)
	got := futureRows["heroes"].([]any)
	if len(got) != 2 || got[0].(map[string]any)["zh"] != "本机灵魅" || got[1].(map[string]any)["zh"] != "新版英雄" {
		t.Fatalf("merged glossary rows = %#v", got)
	}
}
