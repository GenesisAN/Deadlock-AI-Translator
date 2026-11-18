package translate

import (
	"context"
	"strings"
	"testing"

	"deadlocktranslator/internal/assets"
	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

func TestFuzzyCandidatesReachAIWithoutForcingTranslation(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	book := (&Lexicon{base: map[string]term{
		"子弹护甲": {match: "子弹护甲", en: "Bullet Armor", zh: "子弹护甲"},
	}}).withGlossary("")
	var sent provider.Input
	engine := New(translatorFunc(func(in provider.Input) provider.Output {
		sent = in
		return provider.Output{Translation: "Buy Bullet Armor"}
	}), book)
	snap := settings.Snapshot{Config: settings.Config{
		Provider: "compatible", Timeout: 5000, MaxConcurrent: 2,
		Languages: settings.Languages{Source: "zh-Hans", Target: "en"},
	}}
	out, detail, err := engine.Run(context.Background(), snap, Request{Text: "买子弹甲"}, &snap.Config.Context, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Translation != "Buy Bullet Armor" || sent.Text != "买子弹甲" || sent.HasPlaceholders {
		t.Fatalf("fuzzy candidate forced a translation: output=%q input=%+v", out.Translation, sent)
	}
	if len(sent.Candidates) == 0 || len(sent.Candidates) > 8 ||
		!strings.Contains(strings.Join(sent.Candidates, "\n"), "子弹护甲 = Bullet Armor") {
		t.Fatalf("candidates for 买子弹甲 = %q, request = %+v", sent.Candidates, sent)
	}
	if detail == nil || strings.Join(detail.Candidates, "\n") != strings.Join(sent.Candidates, "\n") {
		t.Fatalf("preview candidates = %+v, request candidates = %q", detail, sent.Candidates)
	}
	if system := provider.Messages(sent)[0].Content; !strings.Contains(system, "Possible terminology") || !strings.Contains(system, sent.Candidates[0]) {
		t.Fatalf("AI prompt omitted ranked candidates: %q", system)
	}
}

func TestFuzzyCandidatesLeaveExactMatchesAndNonAIAlone(t *testing.T) {
	book := (&Lexicon{base: map[string]term{
		"子弹抗性": {match: "子弹抗性", en: "Bullet Resist", zh: "子弹抗性"},
		"子弹坚甲": {match: "子弹坚甲", en: "Bullet Resilience", zh: "子弹坚甲"},
	}}).withGlossary("")
	exact := book.prepare("买子弹抗性", "en", true)
	if len(exact.Protected) != 1 || len(exact.Candidates) != 0 {
		t.Fatalf("exact term should stay protected: %+v", exact)
	}
	fuzzy := book.prepare("买子弹甲", "en", false)
	if len(fuzzy.Candidates) != 0 || len(fuzzy.Protected) != 0 {
		t.Fatalf("non-AI provider received fuzzy suggestions: %+v", fuzzy)
	}
	noise := book.prepare("晚上见", "en", true)
	if len(noise.Candidates) != 0 {
		t.Fatalf("unrelated candidates = %q", noise.Candidates)
	}
	english := (&Lexicon{base: map[string]term{
		"bullet armor": {match: "bullet armor", en: "Bullet Armor", zh: "子弹护甲"},
	}}).withGlossary("").prepare("buy bullet armr", "zh-Hans", true)
	if !strings.Contains(strings.Join(english.Candidates, "\n"), "bullet armor = 子弹护甲") {
		t.Fatalf("English typo candidates = %q", english.Candidates)
	}
}

func TestProjectLexiconPromptsBothHexItems(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	book, err := NewProjectLexicon(assets.Files, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := book.lookup("buy hex ,", "en", "zh-Hans"); got != "" {
		t.Fatalf("project glossary forced buy hex to %q", got)
	}
	prepared := book.prepare("buy hex ,", "zh-Hans", true)
	if len(prepared.Protected) != 0 {
		t.Fatalf("project glossary forced a hex term: %+v", prepared.Protected)
	}
	candidates := strings.Join(prepared.Candidates, "\n")
	for _, want := range []string{"slowing hex = [equipment] Slowing Hex / 减速魔咒", "disarming hex = [equipment] Disarming Hex / 缴械魔咒"} {
		if !strings.Contains(candidates, want) {
			t.Fatalf("missing %q in project candidates: %q", want, candidates)
		}
	}
	request := provider.Input{Text: prepared.Text, Candidates: prepared.Candidates}
	if system := provider.Messages(request)[0].Content; !strings.Contains(system, "instead of guessing") ||
		!strings.Contains(system, "slowing hex = [equipment] Slowing Hex / 减速魔咒") || !strings.Contains(system, "disarming hex = [equipment] Disarming Hex / 缴械魔咒") {
		t.Fatalf("project AI prompt did not preserve the choice: %q", system)
	}
}

func BenchmarkFuzzyCandidates(b *testing.B) {
	book, err := NewProjectLexicon(assets.Files, b.TempDir(), b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		book.prepare("买子弹甲", "en", true)
	}
}
