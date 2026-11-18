package translate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"deadlocktranslator/internal/provider"
	"deadlocktranslator/internal/settings"
)

type dictionary map[string]map[string]string
type userDictionary struct {
	User    dictionary `json:"user"`
	Learned dictionary `json:"learned"`
}
type term struct{ match, en, zh, reading, id string }
type Lexicon struct {
	version                     int
	personal                    string
	custom, game, user, learned dictionary
	base                        map[string]term
	ambiguous                   map[string]bool
	aliases                     map[string]string
	entries                     map[string]GlossaryEntry
	matcher                     map[rune][]term
}

func loadUserDictionary(dir, legacyDir string) (userDictionary, error) {
	var user userDictionary
	for _, p := range []string{filepath.Join(dir, "dictionary.json"), filepath.Join(legacyDir, "dictionary.json")} {
		b, e := os.ReadFile(p)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return user, e
		}
		if len(b) > 2<<20 {
			return user, errors.New("dictionary.json 超过 2 MiB")
		}
		if e = json.Unmarshal(b, &user); e != nil {
			return user, e
		}
		break
	}
	user.User, user.Learned = normalizedBook(user.User), normalizedBook(user.Learned)
	return user, nil
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
	for _, line := range strings.Split(strings.TrimSpace(gloss+"\n"+l.personal), "\n") {
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
		for _, d := range []dictionary{l.custom, l.user, l.game, l.learned} {
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
	Text       string
	Hints      []string
	Terms      []provider.TermReference
	Candidates []string
	Protected  []protected
}

var tokens = regexp.MustCompile(`(?i)LCTPH\s*([0-9]+)`)
var errInvalidPlaceholder = errors.New("译文中的术语占位符丢失、重复或意外出现")

func asciiWord(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' }
func boundary(text, key string, i int) bool {
	end := i + len(key)
	return !(i > 0 && asciiWord(key[0]) && asciiWord(text[i-1])) &&
		!(end < len(text) && asciiWord(key[len(key)-1]) && asciiWord(text[end]))
}

func embeddedSingleHanHeroAlias(text string, i int, match term, entries map[string]GlossaryEntry) bool {
	entry, ok := entries[match.id]
	if !ok || entry.Category != "heroes" || utf8.RuneCountInString(match.match) != 1 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(match.match)
	if !unicode.Is(unicode.Han, r) {
		return false
	}
	end := i + len(match.match)
	if i > 0 {
		previous, _ := utf8.DecodeLastRuneInString(text[:i])
		if unicode.Is(unicode.Han, previous) {
			return true
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		return unicode.Is(unicode.Han, next)
	}
	return false
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
	p := preparedText{Text: text, Hints: []string{}, Terms: []provider.TermReference{}, Candidates: []string{}, Protected: []protected{}}
	if tokens.MatchString(text) {
		return p
	}
	code := side(target)
	if code != "en" && code != "zh" {
		return p
	}
	search, index := searchText(text)
	var out strings.Builder
	var unmatched strings.Builder
	cursor := 0
	unmatchedCursor := 0
	seen := map[string]bool{}
	hintBudget := 450
	termBudget := 900
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
		// A one-character hero nickname inside Chinese text is only a possible
		// reading. Keep the full sentence intact so AI can choose its meaning.
		if embeddedSingleHanHeroAlias(search, i, match, l.entries) {
			if ai && !seen[match.match] && len(p.Candidates) < 8 {
				entry := l.entries[match.id]
				p.Candidates = append(p.Candidates, match.match+" = [possible hero] "+entry.En+" / "+entry.Zh)
				seen[match.match] = true
			}
			i += n
			continue
		}
		end := i + len(match.match)
		original := text[index[i]:index[end]]
		aliasHintOnly := ai && l.curatedAlias(match.match, target)
		if match.reading == "" {
			unmatched.WriteString(text[unmatchedCursor:index[i]])
			unmatchedCursor = index[end]
		}
		label := match.en
		if code == "zh" {
			label = match.zh
		}
		entry, structured := l.entries[match.id]
		if ai && structured && !seen[match.match] && len(p.Terms) < 8 {
			ref := provider.TermReference{
				Matched: original, ID: entry.ID, Category: entry.Category,
				En: entry.En, Zh: entry.Zh, Aliases: entry.Aliases,
			}
			for _, alias := range entry.KeepAliases {
				if normalize(alias) == match.match {
					ref.KeepOriginal = true
					break
				}
			}
			encoded, _ := json.Marshal(ref)
			if n := settings.Size(string(encoded)); n <= termBudget {
				p.Terms = append(p.Terms, ref)
				termBudget -= n + 1
				seen[match.match] = true
			}
		}
		hint := match.reading
		if !structured && hint == "" && label != "" && normalize(label) != match.match {
			hint = label
		}
		if ai && !structured && hint != "" && !seen[match.match] && len(p.Hints) < 8 {
			line := original + " = " + hint
			if n := settings.Size(line); n <= hintBudget {
				p.Hints = append(p.Hints, line)
				hintBudget -= n + 1
				seen[match.match] = true
			}
		}
		if !aliasHintOnly && match.reading == "" && label != "" && len(p.Protected) < 32 {
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
	if ai {
		unmatched.WriteString(text[unmatchedCursor:])
		p.Candidates = append(p.Candidates, l.candidateHints(unmatched.String(), code)...)
		if len(p.Candidates) > 8 {
			p.Candidates = p.Candidates[:8]
		}
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
