package translate

import (
	"testing"

	"deadlocktranslator/internal/assets"
)

func TestPreferredChatAbbreviation(t *testing.T) {
	t.Setenv("BABEL_TOWER_DICT_ENABLED", "true")
	book, err := NewProjectLexicon(assets.Files, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := book.lookup("BKB", "en", "zh-Hans"); got != "BKB" {
		t.Errorf("lookup(BKB) = %q", got)
	}

	prepared := book.prepare("I need BKB now", "zh-Hans", true)
	if len(prepared.Terms) != 1 || prepared.Terms[0].ID != "upgrade_unstoppable" || !prepared.Terms[0].KeepOriginal || len(prepared.Protected) != 0 {
		t.Fatalf("abbreviation was not passed to AI as a retained alias: %+v", prepared)
	}
	nonAI := book.prepare("I need BKB now", "zh-Hans", false)
	if len(nonAI.Protected) != 1 || nonAI.Protected[0].Translation != "BKB" {
		t.Fatalf("non-AI abbreviation was not protected: %+v", nonAI)
	}
	if got, err := nonAI.restore("我现在需要 LCTPH0"); err != nil || got != "我现在需要 BKB" {
		t.Fatalf("restored = %q, %v", got, err)
	}

	withUser := buildCategoryLexicon(2, []categoryGlossary{{Entries: []GlossaryEntry{{ID: "upgrade_unstoppable", Category: "equipment", En: "Unstoppable", Zh: "势不可挡", Aliases: map[string][]string{"en": {"BKB"}}, KeepAliases: []string{"BKB"}}}}}, dictionary{"zh": {"bkb": "黑皇杖"}}, nil)
	if got := withUser.lookup("BKB", "en", "zh-Hans"); got != "黑皇杖" {
		t.Errorf("user translation = %q", got)
	}
	if got := book.withGlossary("BKB = 自定义").lookup("BKB", "en", "zh-Hans"); got != "自定义" {
		t.Errorf("custom translation = %q", got)
	}
	customSentence := book.withGlossary("BKB = 自定义").prepare("buy BKB now", "zh-Hans", true)
	if len(customSentence.Protected) != 1 || customSentence.Protected[0].Translation != "自定义" {
		t.Errorf("custom translation in a sentence = %+v", customSentence)
	}
}
