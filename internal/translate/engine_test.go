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
