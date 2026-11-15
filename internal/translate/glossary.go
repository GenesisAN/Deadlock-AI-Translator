package translate

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"deadlocktranslator/internal/settings"
)

type dictionary map[string]map[string]string
type glossaryRow struct {
	En, Zh  string
	Aliases map[string][]struct{ Text string }
}

type pack struct {
	User     dictionary
	Learned  dictionary
	Glossary struct {
		Heroes, Items, Terms, Abilities []glossaryRow
		Ambiguities                     []struct {
			Text, Language, Note string
			Meanings             []string
		}
	}
}
type term struct{ match, en, zh, reading string }
type Lexicon struct {
	custom, builtin, game, user, learned dictionary
	base                                 map[string]term
	ambiguous                            map[string]bool
	matcher                              map[rune][]term
}

func load(files fs.FS, name string, dst any) error {
	b, e := fs.ReadFile(files, name)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, dst)
}

func NewLexicon(assets fs.FS, dir, legacyDir string) (*Lexicon, error) {
	l := &Lexicon{base: map[string]term{}, ambiguous: map[string]bool{}}
	var game, builtin, user pack
	official := map[string]string{}
	if e := load(assets, "dictionaries/deadlock.zh-en.json", &game); e != nil {
		return nil, e
	}
	if e := load(assets, "dictionaries/builtin.json", &builtin); e != nil {
		return nil, e
	}
	if e := load(assets, "dictionaries/gamenames.json", &official); e != nil {
		return nil, e
	}
	for _, p := range []string{filepath.Join(dir, "dictionary.json"), filepath.Join(legacyDir, "dictionary.json")} {
		b, e := os.ReadFile(p)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if len(b) > 2<<20 {
			return nil, errors.New("dictionary.json 超过 2 MiB")
		}
		if e = json.Unmarshal(b, &user); e != nil {
			return nil, e
		}
		break
	}
	l.game, l.builtin, l.user, l.learned = normalizedBook(game.User), normalizedBook(builtin.User), normalizedBook(user.User), normalizedBook(user.Learned)
	add := func(alias, en, zh string) {
		k := normalize(alias)
		if k == "" || en == "" || zh == "" {
			return
		}
		if _, ok := l.base[k]; !ok {
			l.base[k] = term{match: k, en: en, zh: zh}
		}
	}
	for _, rows := range [][]glossaryRow{game.Glossary.Heroes, game.Glossary.Items, game.Glossary.Terms} {
		for _, e := range rows {
			add(e.En, e.En, e.Zh)
			add(e.Zh, e.En, e.Zh)
		}
	}
	for _, rows := range [][]glossaryRow{game.Glossary.Heroes, game.Glossary.Items, game.Glossary.Terms} {
		for _, e := range rows {
			for _, lang := range []string{"en", "zh"} {
				for _, a := range e.Aliases[lang] {
					if lang == "en" && settings.Size(a.Text) < 3 && normalize(a.Text) != "cd" {
						continue
					}
					other := "en"
					if lang == "en" {
						other = "zh"
					}
					if l.game[other][normalize(a.Text)] != "" {
						add(a.Text, e.En, e.Zh)
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(official))
	for en := range official {
		keys = append(keys, en)
	}
	sort.Strings(keys)
	for _, en := range keys {
		zh := official[en]
		if en != "english" && en != "schinese" {
			add(en, en, zh)
			add(zh, en, zh)
		}
	}
	for _, a := range game.Glossary.Ambiguities {
		key := normalize(a.Text)
		if key == "" || len(a.Meanings) < 2 {
			continue
		}
		l.ambiguous[key] = true
		l.base[key] = term{match: key, reading: strings.Join(a.Meanings, " / ") + "。" + a.Note}
	}
	l.base["卡牌"] = term{match: "卡牌", reading: "Wraith when targeting a hero (kill/focus); Card Trick when discussing the ability"}
	l.base["tp"] = term{match: "tp", reading: "teleport in a movement callout; Torment Pulse in an item build"}
	l.base["kd"] = term{match: "kd", reading: "Knockdown (anti-air), Kinetic Dash (movement/item build), or kill/death ratio (score)"}
	for _, key := range []string{"卡牌", "tp", "kd"} {
		l.ambiguous[key] = true
	}
	if strings.EqualFold(os.Getenv("BABEL_TOWER_DICT_ENABLED"), "false") {
		l.game, l.builtin, l.user, l.learned = nil, nil, nil, nil
	}
	return l.withGlossary(""), nil
}

func normalizedBook(book dictionary) dictionary {
	for lang, d := range book {
		next := map[string]string{}
		keys := make([]string, 0, len(d))
		for k := range d {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next[normalize(k)] = d[k]
		}
		book[lang] = next
	}
	return book
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(s, "‘", "'"), "’", "'")), " "))
}
func side(code string) string {
	code = strings.ToLower(code)
	p, _, _ := strings.Cut(code, "-")
	if p == "zh" && code != "zh" && code != "zh-hans" && code != "zh-cn" && code != "zh-sg" {
		return code
	}
	return p
}
func (l *Lexicon) withGlossary(gloss string) *Lexicon {
	out := *l
	out.custom = dictionary{"en": {}, "zh": {}}
	unique := make(map[string]term, len(l.base))
	for key, t := range l.base {
		unique[key] = t
	}
	for _, line := range strings.Split(gloss, "\n") {
		en, zh, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(en) == "" || strings.TrimSpace(zh) == "" {
			continue
		}
		en, zh = strings.TrimSpace(en), strings.TrimSpace(zh)
		e, z := normalize(en), normalize(zh)
		out.custom["zh"][e] = zh
		unique[e] = term{match: e, en: en, zh: zh}
		if _, exists := out.custom["en"][z]; !exists {
			out.custom["en"][z] = en
			unique[z] = term{match: z, en: en, zh: zh}
		}
	}
	out.matcher = map[rune][]term{}
	for _, t := range unique {
		first, _ := utf8.DecodeRuneInString(t.match)
		out.matcher[first] = append(out.matcher[first], t)
	}
	for _, list := range out.matcher {
		sort.Slice(list, func(i, j int) bool { return len(list[i].match) > len(list[j].match) })
	}
	return &out
}
func (l *Lexicon) lookup(s, src, dst string) string {
	k := normalize(s)
	short := strings.TrimFunc(k, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	target := side(dst)
	if source := side(src); source != "auto" && source == target {
		return ""
	}
	for _, key := range []string{k, short} {
		if key == "" {
			continue
		}
		for _, d := range []dictionary{l.custom, l.user, l.game, l.builtin, l.learned} {
			if l.ambiguous[key] && d != nil {
				if v := l.custom[target][key]; v != "" {
					return v
				}
				if v := l.user[target][key]; v != "" {
					return v
				}
				break
			}
			if v := d[target][key]; v != "" {
				return v
			}
		}
	}
	return ""
}

type protected struct {
	Token       string `json:"token"`
	Translation string `json:"translation"`
}
type preparedText struct {
	Text      string
	Hints     []string
	Protected []protected
}

var tokens = regexp.MustCompile(`(?i)LCTPH\s*([0-9]+)`)
var errInvalidPlaceholder = errors.New("译文中的术语占位符丢失、重复或意外出现")

func asciiWord(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' }
func boundary(text, key string, i int) bool {
	end := i + len(key)
	return !(i > 0 && asciiWord(key[0]) && asciiWord(text[i-1])) &&
		!(end < len(text) && asciiWord(key[len(key)-1]) && asciiWord(text[end]))
}

// Map the normalized search bytes back onto the original input. Translation keeps the original spelling and spacing outside matches.
func searchText(text string) (string, []int) {
	var b strings.Builder
	offset := make([]int, 0, len(text)+1)
	inSpace := false
	for i, r := range text {
		if unicode.IsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
				offset = append(offset, i)
			}
			inSpace = true
			continue
		}
		inSpace = false
		if r == '‘' || r == '’' {
			r = '\''
		}
		r = unicode.ToLower(r)
		b.WriteRune(r)
		for n := 0; n < utf8.RuneLen(r); n++ {
			offset = append(offset, i)
		}
	}
	return b.String(), append(offset, len(text))
}

func (l *Lexicon) prepare(text, target string, ai bool) preparedText {
	p := preparedText{Text: text, Hints: []string{}, Protected: []protected{}}
	if tokens.MatchString(text) {
		return p
	}
	code := side(target)
	if code != "en" && code != "zh" {
		return p
	}
	search, index := searchText(text)
	var out strings.Builder
	cursor := 0
	seen := map[string]bool{}
	hintBudget := 450
	for i := 0; i < len(search); {
		r, n := utf8.DecodeRuneInString(search[i:])
		match := term{}
		for _, candidate := range l.matcher[r] {
			if strings.HasPrefix(search[i:], candidate.match) && boundary(search, candidate.match, i) {
				match = candidate
				break
			}
		}
		if match.match == "" {
			i += n
			continue
		}
		end := i + len(match.match)
		original := text[index[i]:index[end]]
		label := match.en
		if code == "zh" {
			label = match.zh
		}
		hint := match.reading
		if hint == "" && label != "" && normalize(label) != match.match {
			hint = label
		}
		if ai && hint != "" && !seen[match.match] && len(p.Hints) < 8 {
			line := original + " = " + hint
			if n := settings.Size(line); n <= hintBudget {
				p.Hints = append(p.Hints, line)
				hintBudget -= n + 1
				seen[match.match] = true
			}
		}
		if match.reading == "" && label != "" && len(p.Protected) < 32 {
			t := "LCTPH" + strconv.Itoa(len(p.Protected))
			out.WriteString(text[cursor:index[i]])
			out.WriteString(t)
			cursor = index[end]
			p.Protected = append(p.Protected, protected{t, label})
		}
		i = end
	}
	if len(p.Protected) > 0 {
		out.WriteString(text[cursor:])
		p.Text = out.String()
	}
	return p
}

func (p preparedText) restore(s string) (string, error) {
	if len(p.Protected) == 0 {
		matches := tokens.FindAllStringSubmatch(s, -1)
		if len(matches) == 0 {
			return s, nil
		}
		// The source can mention a marker literally. Allow only its original occurrences.
		remaining := map[string]int{}
		for _, match := range tokens.FindAllStringSubmatch(p.Text, -1) {
			remaining[match[1]]++
		}
		for _, match := range matches {
			id := match[1]
			if remaining[id] == 0 {
				return "", errInvalidPlaceholder
			}
			remaining[id]--
		}
		return s, nil
	}
	counts := make([]int, len(p.Protected))
	invalid := false
	out := tokens.ReplaceAllStringFunc(s, func(m string) string {
		sub := tokens.FindStringSubmatch(m)
		i, e := strconv.Atoi(sub[1])
		if e != nil || i >= len(counts) {
			invalid = true
			return m
		}
		counts[i]++
		return p.Protected[i].Translation
	})
	for _, n := range counts {
		if n != 1 {
			invalid = true
		}
	}
	if invalid {
		return "", errInvalidPlaceholder
	}
	return out, nil
}
