package translate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var categoryGlossaries = [...]string{
	"equipment", "heroes", "map", "units", "events", "actions", "chat-abbreviations",
}

// GlossaryEntry identifies one game or chat concept independently of the
// language and spelling used to mention it.
type GlossaryEntry struct {
	ID          string              `json:"id"`
	Category    string              `json:"category"`
	En          string              `json:"en"`
	Zh          string              `json:"zh"`
	Aliases     map[string][]string `json:"aliases,omitempty"`
	KeepAliases []string            `json:"keepAliases,omitempty"`
}

type categoryGlossary struct {
	Entries []GlossaryEntry `json:"entries"`
}

var glossaryID = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

func parseCategoryGlossary(category string, data []byte) (categoryGlossary, error) {
	var glossary categoryGlossary
	if err := json.Unmarshal(data, &glossary); err != nil {
		return glossary, err
	}
	if len(glossary.Entries) == 0 {
		return glossary, errors.New("词库不能为空")
	}
	seen := map[string]string{}
	ids := map[string]bool{}
	for i, row := range glossary.Entries {
		if row.Category != category {
			return glossary, fmt.Errorf("第 %d 条分类应为 %s", i+1, category)
		}
		if !glossaryID.MatchString(row.ID) || ids[row.ID] {
			return glossary, fmt.Errorf("第 %d 条 ID 无效或重复：%s", i+1, row.ID)
		}
		ids[row.ID] = true
		if normalize(row.En) == "" || normalize(row.Zh) == "" {
			return glossary, fmt.Errorf("第 %d 条缺少英文或中文", i+1)
		}
		key := normalize(row.En)
		if seen[key] != "" {
			return glossary, fmt.Errorf("第 %d 条与 %s 重复：%s", i+1, seen[key], row.En)
		}
		seen[key] = row.En
		aliasKeys := map[string]bool{}
		for lang, values := range row.Aliases {
			if lang != "en" && lang != "zh" {
				return glossary, fmt.Errorf("第 %d 条的别名语言无效", i+1)
			}
			for _, value := range values {
				if normalize(value) == "" {
					return glossary, fmt.Errorf("第 %d 条有空别名", i+1)
				}
				aliasKeys[normalize(value)] = true
			}
		}
		for _, value := range row.KeepAliases {
			if !aliasKeys[normalize(value)] {
				return glossary, fmt.Errorf("第 %d 条保留的简称不在别名中：%s", i+1, value)
			}
		}
	}
	return glossary, nil
}

func loadCategoryGlossaries(read func(string) (string, error)) ([]categoryGlossary, error) {
	books := make([]categoryGlossary, 0, len(categoryGlossaries))
	seen := map[string]string{}
	ids := map[string]bool{}
	for _, name := range categoryGlossaries {
		content, err := read(name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		book, err := parseCategoryGlossary(name, []byte(content))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, row := range book.Entries {
			if ids[row.ID] {
				return nil, fmt.Errorf("%s: 重复的词条 ID %s", name, row.ID)
			}
			ids[row.ID] = true
			for _, source := range append([]string{row.En}, row.Aliases["en"]...) {
				key := "en:" + normalize(source)
				if previous := seen[key]; previous != "" && previous != row.En {
					return nil, fmt.Errorf("%s: %q 同时指向 %s 和 %s", name, source, previous, row.En)
				}
				seen[key] = row.En
			}
			for _, source := range append([]string{row.Zh}, row.Aliases["zh"]...) {
				key := "zh:" + normalize(source)
				if previous := seen[key]; previous != "" && previous != row.En {
					return nil, fmt.Errorf("%s: %q 同时指向 %s 和 %s", name, source, previous, row.En)
				}
				seen[key] = row.En
			}
		}
		books = append(books, book)
	}
	return books, nil
}

func buildCategoryLexicon(version int, books []categoryGlossary, user, learned dictionary) *Lexicon {
	l := &Lexicon{
		version: version, base: map[string]term{}, ambiguous: map[string]bool{},
		aliases: map[string]string{}, entries: map[string]GlossaryEntry{},
		game: dictionary{"en": {}, "zh": {}}, user: user, learned: learned,
	}
	add := func(source string, row GlossaryEntry, target string) {
		key := normalize(source)
		if key == "" || l.game[target][key] != "" {
			return
		}
		label := row.En
		if target == "zh" {
			label = row.Zh
		}
		l.game[target][key] = label
		if _, exists := l.base[key]; !exists {
			l.base[key] = term{match: key, en: row.En, zh: row.Zh, id: row.ID}
		}
	}
	// Canonical terms take precedence over aliases in every category.
	for _, book := range books {
		for _, row := range book.Entries {
			l.entries[row.ID] = row
			add(row.En, row, "zh")
			add(row.Zh, row, "en")
		}
	}
	for _, book := range books {
		for _, row := range book.Entries {
			keep := map[string]bool{}
			for _, alias := range row.KeepAliases {
				keep[normalize(alias)] = true
			}
			for _, alias := range row.Aliases["en"] {
				add(alias, row, "zh")
				key := normalize(alias)
				l.aliases[key] = row.ID
				if keep[key] {
					l.game["zh"][key] = alias
					l.base[key] = term{match: key, en: alias, zh: alias, id: row.ID}
				}
			}
			for _, alias := range row.Aliases["zh"] {
				add(alias, row, "en")
				key := normalize(alias)
				l.aliases[key] = row.ID
				if keep[key] {
					l.game["en"][key] = alias
					l.base[key] = term{match: key, en: alias, zh: alias, id: row.ID}
				}
			}
		}
	}
	// Bare "mid" is used for both the middle lane and Mid-Boss. Longer calls
	// such as "do mid" still resolve through their explicit action entries.
	l.ambiguous["mid"] = true
	l.base["mid"] = term{match: "mid", reading: "mid 在报线时指中路；do mid 指打中区头目"}
	// External user entries override bundled terms in both exact and sentence matches.
	for _, target := range []string{"zh", "en"} {
		keys := make([]string, 0, len(user[target]))
		for key := range user[target] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			label := user[target][key]
			if normalize(key) == "" || normalize(label) == "" {
				continue
			}
			if target == "zh" {
				l.base[normalize(key)] = term{match: normalize(key), en: key, zh: label}
			} else {
				l.base[normalize(key)] = term{match: normalize(key), en: label, zh: key}
			}
		}
	}
	if strings.EqualFold(os.Getenv("BABEL_TOWER_DICT_ENABLED"), "false") {
		l.game, l.user, l.learned = nil, nil, nil
	}
	return l.withGlossary("")
}

func (l *Lexicon) curatedAlias(text, target string) bool {
	key := normalize(text)
	short := strings.TrimFunc(key, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if l.aliases[key] == "" {
		key = short
		if l.aliases[key] == "" {
			return false
		}
	}
	targetSide := side(target)
	return l.custom[targetSide][key] == "" && l.user[targetSide][key] == ""
}
