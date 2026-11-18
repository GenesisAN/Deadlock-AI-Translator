package translate

import (
	"context"
	"strings"
	"testing"

	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

type translatorFunc func(provider.Input) provider.Output

func (f translatorFunc) Translate(_ context.Context, p provider.Preset, _ provider.Settings, _ string, in provider.Input) (provider.Output, error) {
	out := f(in)
	out.Provider = p.ID
	return out, nil
}

func TestEnginePlaceholders(t *testing.T) {
	book := (&Lexicon{}).withGlossary("Wraith = 灵魅\nBullet Resist Shredder = 粉碎护甲")
	snap := settings.Snapshot{Config: settings.Config{
		Provider: "compatible", Timeout: 5000, MaxConcurrent: 2,
		Languages: settings.Languages{Source: "en", Target: "zh-Hans"},
	}}
	for _, tc := range []struct {
		name, source, reply, want string
		protected, invalid        bool
	}{
		{name: "ordinary", source: "go back now", reply: "现在回去", want: "现在回去"},
		{name: "invented", source: "go back now", reply: "现在回去 LCTPH1", invalid: true},
		{name: "invented lower case", source: "go back now", reply: "lctph0 现在回去", invalid: true},
		{name: "invented with spacing", source: "go back now", reply: "回去(lCtPh 12)", invalid: true},
		{name: "invented twice", source: "go back now", reply: "LCTPH0 回去 LCTPH0", invalid: true},
		{name: "restored", source: "Wraith needs Bullet Resist Shredder", reply: "LCTPH0 需要 LCTPH1", want: "灵魅 需要 粉碎护甲", protected: true},
		{name: "missing", source: "focus Wraith", reply: "集火灵魅", protected: true, invalid: true},
		{name: "duplicate", source: "focus Wraith", reply: "集火 LCTPH0 (lctph 0)", protected: true, invalid: true},
		{name: "unassigned", source: "focus Wraith", reply: "集火 LCTPH0 LCTPH1", protected: true, invalid: true},
		{name: "literal", source: "type LCTPH1 here", reply: "在这里输入 LCTPH1", want: "在这里输入 LCTPH1"},
		{name: "duplicated literal", source: "type LCTPH1 here", reply: "在这里输入 LCTPH1 LCTPH1", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent provider.Input
			engine := New(translatorFunc(func(in provider.Input) provider.Output {
				sent = in
				return provider.Output{Translation: tc.reply}
			}), book)
			out, _, err := engine.Run(context.Background(), snap, Request{Text: tc.source}, nil, false, false)
			if (err != nil) != tc.invalid {
				t.Fatalf("error = %v, want placeholder error: %t", err, tc.invalid)
			}
			if tc.invalid && !strings.Contains(err.Error(), errInvalidPlaceholder.Error()) {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Translation != tc.want {
				t.Errorf("translation = %q, want %q", out.Translation, tc.want)
			}
			system := provider.Messages(sent)[0].Content
			if got := strings.Contains(system, "LCTPH"); got != tc.protected {
				t.Errorf("placeholder instruction present = %t, want %t, prompt = %q", got, tc.protected, system)
			}
		})
	}
}

func TestChatHistoryReachesAIAndSeparatesCacheEntries(t *testing.T) {
	book := (&Lexicon{}).withGlossary("")
	var inputs []provider.Input
	engine := New(translatorFunc(func(in provider.Input) provider.Output {
		inputs = append(inputs, in)
		return provider.Output{Translation: "go mid"}
	}), book)
	snap := settings.Snapshot{Config: settings.Config{
		Provider: "compatible", Timeout: 5000, MaxConcurrent: 2,
		Languages: settings.Languages{Source: "zh-Hans", Target: "en"},
	}}
	first := []provider.ChatMessage{{Direction: "incoming", Text: "猪在中路"}, {Direction: "outgoing", Text: "我去抓他"}}
	second := []provider.ChatMessage{{Direction: "incoming", Text: "羊在中路"}}
	for _, history := range [][]provider.ChatMessage{first, second, first} {
		if _, _, err := engine.Run(context.Background(), snap, Request{Text: "来了", History: history}, nil, false, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(inputs) != 2 || len(inputs[0].History) != 2 || inputs[0].History[0].Text != "猪在中路" || inputs[1].History[0].Text != "羊在中路" {
		t.Fatalf("history did not reach AI or separate cached translations: %+v", inputs)
	}
	messages := provider.Messages(inputs[0])
	if len(messages) != 2 || messages[1].Content != "来了" || !strings.Contains(messages[0].Content, `"direction":"incoming","text":"猪在中路"`) ||
		!strings.Contains(messages[0].Content, "translate only the current user message") {
		t.Fatalf("AI message mixed history and current text: %+v", messages)
	}
	snap.Config.Provider = "bing"
	plainCalls := 0
	plain := New(translatorFunc(func(in provider.Input) provider.Output {
		plainCalls++
		if len(in.History) != 0 {
			t.Errorf("literal provider received AI history: %+v", in.History)
		}
		return provider.Output{Translation: "go mid"}
	}), book)
	if _, _, err := plain.Run(context.Background(), snap, Request{Text: "来了", History: first}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := plain.Run(context.Background(), snap, Request{Text: "来了", History: second}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if plainCalls != 1 {
		t.Fatalf("literal provider cache depended on unused history: %d calls", plainCalls)
	}
}
