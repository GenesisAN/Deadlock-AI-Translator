package translate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"deadlocktranslator/internal/assets"
	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

func TestCuratedCategoryGlossaries(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	book, err := NewProjectLexicon(assets.Files, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		text, source, target, want string
	}{
		{"Unstoppable", "en", "zh", "势不可挡"},
		{"BKB", "en", "zh", "BKB"},
		{"BRS", "en", "zh", "粉碎护甲"},
		{"QSR", "en", "zh", "魔力装填"},
		{"Wraith", "en", "zh", "灵魅"},
		{"卡牌", "zh", "en", "Wraith"},
		{"卡牌姐", "zh", "en", "Wraith"},
		{"机器人", "zh", "en", "Bebop"},
		{"独角兽", "zh", "en", "Celeste"},
		{"哈基米娜", "zh", "en", "Mina"},
		{"护士", "zh", "en", "Nurse Harrow"},
		{"刀姐", "zh", "en", "Yamato"},
		{"McG", "en", "zh", "麦金妮"},
		{"Para", "en", "zh", "悖论"},
		{"blue lane", "en", "zh", "百老汇"},
		{"大龙", "zh", "en", "Mid-Boss"},
		{"中野", "zh", "en", "Medium Haunt"},
		{"大龙复活", "zh", "en", "Mid-Boss Arrived"},
		{"BUFF石刷新", "zh", "en", "Bridge Buff Here"},
		{"分开推塔", "zh", "en", "split push"},
		{"omw", "en", "zh", "我在路上"},
		{"4.8k spike", "en", "zh", ""},
		{"red lane", "en", "zh", ""},
		{"mid", "en", "zh", ""},
	} {
		if got := book.lookup(tc.text, tc.source, tc.target); got != tc.want {
			t.Errorf("lookup(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
	if prepared := book.prepare("mid", "zh", true); len(prepared.Protected) != 0 || len(prepared.Hints) == 0 {
		t.Errorf("ambiguous mid was forced: %+v", prepared)
	}
	if prepared := book.prepare("do mid", "zh", true); len(prepared.Protected) != 0 || len(prepared.Terms) != 1 || prepared.Terms[0].Category != "actions" || prepared.Terms[0].En != "take mid boss" {
		t.Errorf("explicit mid boss callout: %+v", prepared)
	}
	if got := book.withGlossary("BKB = 黑皇杖").lookup("BKB", "en", "zh"); got != "黑皇杖" {
		t.Errorf("custom BKB translation = %q", got)
	}
	if candidates := strings.Join(book.prepare("buy quicksilver reloa", "zh", true).Candidates, "\n"); !strings.Contains(candidates, "[equipment] Quicksilver Reload / 魔力装填") {
		t.Errorf("category missing from fuzzy lookup: %s", candidates)
	}
}

func TestCuratedRosterAndShop(t *testing.T) {
	for name, want := range map[string]int{"heroes": 44, "equipment": 156} {
		data, err := assets.Files.ReadFile("dictionaries/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var glossary categoryGlossary
		if err := json.Unmarshal(data, &glossary); err != nil {
			t.Fatal(err)
		}
		if len(glossary.Entries) != want {
			t.Errorf("%s entries = %d, want %d", name, len(glossary.Entries), want)
		}
	}
}

func TestSingleCharacterHeroCallouts(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	book, err := NewProjectLexicon(assets.Files, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for alias, hero := range map[string]string{
		"羊": "Billy", "牛": "Abrams", "猪": "Mo & Krill", "蛇": "Vyper", "狼": "Silver",
	} {
		if got := book.lookup(alias, "zh", "en"); got != hero {
			t.Errorf("lookup(%q) = %q, want %q", alias, got, hero)
		}
		prepared := book.prepare(alias+"来了", "en", true)
		possible := alias + " = [possible hero] " + hero + " / "
		if len(prepared.Terms) != 0 || len(prepared.Protected) != 0 || !strings.Contains(strings.Join(prepared.Candidates, "\n"), possible) {
			t.Errorf("AI context for %q = %+v", alias, prepared)
		}
	}
	ordinary := book.prepare("猪队友", "en", true)
	if ordinary.Text != "猪队友" || len(ordinary.Terms) != 0 || len(ordinary.Protected) != 0 ||
		!strings.Contains(provider.Messages(provider.Input{Text: ordinary.Text, Candidates: ordinary.Candidates})[0].Content, "猪队友 means a bad teammate") {
		t.Errorf("ordinary phrase was presented as an exact hero match: %+v", ordinary)
	}
	for _, chat := range []string{"牛逼", "猪队友", "狼狈"} {
		if prepared := book.prepare(chat, "en", false); len(prepared.Protected) != 0 {
			t.Errorf("ordinary chat %q was forced to a hero: %+v", chat, prepared)
		}
	}
	if prepared := book.prepare("牛!", "en", false); len(prepared.Protected) != 1 || prepared.Protected[0].Translation != "Abrams" {
		t.Errorf("standalone callout was not protected: %+v", prepared)
	}
}

func TestGlossaryObjectIdentitySurvivesNameChange(t *testing.T) {
	old := map[string]any{"entries": []any{map[string]any{
		"id": "hero_atlas", "category": "heroes", "en": "Abrams", "zh": "亚伯兰",
	}}}
	edited := map[string]any{"entries": []any{map[string]any{
		"id": "hero_atlas", "category": "heroes", "en": "Abrams", "zh": "本机译名",
	}}}
	patch := glossaryDiff(old, edited)
	future := map[string]any{"entries": []any{map[string]any{
		"id": "hero_atlas", "category": "heroes", "en": "Abrams Prime", "zh": "亚伯兰",
	}}}
	mergeGlossary(future, patch)
	row := future["entries"].([]any)[0].(map[string]any)
	if row["en"] != "Abrams Prime" || row["zh"] != "本机译名" {
		t.Fatalf("stable ID did not preserve the local translation: %#v", row)
	}
	if _, err := parseCategoryGlossary("heroes", []byte(`{"entries":[{"id":"hero_atlas","category":"equipment","en":"Abrams","zh":"亚伯兰"}]}`)); err == nil {
		t.Fatal("entry with the wrong category was accepted")
	}
}

func TestCuratedAliasesPromptAIInBothDirections(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	book, err := NewProjectLexicon(assets.Files, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ text, target, matched, category, en, zh string }{
		{"buy BRS", "zh-Hans", "BRS", "equipment", "Bullet Resist Shredder", "粉碎护甲"},
		{"买 BRS", "en", "BRS", "equipment", "Bullet Resist Shredder", "粉碎护甲"},
		{"蓝牛买BRS", "en", "BRS", "equipment", "Bullet Resist Shredder", "粉碎护甲"},
		{"McG is here", "zh-Hans", "McG", "heroes", "McGinnis", "麦金妮"},
		{"蓝牛来了", "en", "蓝牛", "heroes", "Abrams", "亚伯兰"},
		{"刀姐来了", "en", "刀姐", "heroes", "Yamato", "大和"},
		{"buy BKB", "zh-Hans", "BKB", "equipment", "Unstoppable", "势不可挡"},
	} {
		prepared := book.prepare(tc.text, tc.target, true)
		found := false
		for _, term := range prepared.Terms {
			if term.Matched == tc.matched && term.Category == tc.category && term.En == tc.en && term.Zh == tc.zh && term.ID != "" {
				found = true
			}
		}
		if prepared.Text != tc.text || len(prepared.Protected) != 0 || !found {
			t.Errorf("prepare(%q, %q) = %+v", tc.text, tc.target, prepared)
		}
	}
	if prepared := book.prepare("蓝牛买BRS", "en", true); len(prepared.Terms) != 2 {
		t.Errorf("mixed Chinese and English aliases = %+v", prepared)
	}
	if prepared := book.prepare("buy BKB", "zh-Hans", true); len(prepared.Terms) != 1 || !prepared.Terms[0].KeepOriginal ||
		!strings.Contains(provider.Messages(provider.Input{Text: prepared.Text, Terms: prepared.Terms})[0].Content, `"keepOriginal":true`) {
		t.Errorf("BKB keep-original metadata missing: %+v", prepared)
	}
	if prepared := book.prepare("buy BRS", "zh-Hans", false); len(prepared.Protected) != 1 || prepared.Protected[0].Translation != "粉碎护甲" {
		t.Errorf("non-AI alias protection = %+v", prepared)
	}
	if book.withGlossary("BRS = 自定义").curatedAlias("BRS", "zh-Hans") {
		t.Fatal("custom entry should override the curated alias")
	}

	var sent provider.Input
	engine := New(translatorFunc(func(in provider.Input) provider.Output {
		sent = in
		return provider.Output{Translation: "粉碎护甲"}
	}), book)
	snap := settings.Snapshot{Config: settings.Config{
		Provider: "compatible", Timeout: 5000, MaxConcurrent: 2,
		Languages: settings.Languages{Source: "en", Target: "zh-Hans"},
	}}
	out, _, err := engine.Run(context.Background(), snap, Request{Text: "BRS"}, nil, false, false)
	if err != nil || out.Provider == "dictionary" || sent.Text != "BRS" || len(sent.Terms) != 1 ||
		!strings.Contains(provider.Messages(sent)[0].Content, `"category":"equipment","en":"Bullet Resist Shredder","zh":"粉碎护甲"`) {
		t.Fatalf("exact abbreviation bypassed AI: output=%+v, input=%+v, err=%v", out, sent, err)
	}
	sent = provider.Input{}
	out, _, err = engine.Run(context.Background(), snap, Request{Text: "蓝牛", Source: "zh", Target: "en"}, nil, false, false)
	if err != nil || out.Provider == "dictionary" || sent.Text != "蓝牛" ||
		!strings.Contains(provider.Messages(sent)[0].Content, `"matched":"蓝牛","id":"hero_atlas","category":"heroes"`) {
		t.Fatalf("Chinese nickname bypassed AI: output=%+v, input=%+v, err=%v", out, sent, err)
	}
}
