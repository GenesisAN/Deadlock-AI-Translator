package translate

import (
	"sort"
	"strings"
	"unicode"

	"deadlocktranslator/internal/settings"
)

type rankedTerm struct {
	line  string
	score float64
}

// candidateText keeps only the source language's script so short chat commands
// do not drown out a nearby item name. The opposite side of the glossary is
// selected by the requested target language.
func candidateText(text, target string) string {
	var out strings.Builder
	for _, r := range normalize(text) {
		if target == "en" && unicode.Is(unicode.Han, r) ||
			target == "zh" && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func characterSet(text string) map[rune]bool {
	set := make(map[rune]bool, len(text))
	for _, r := range text {
		if r != ' ' {
			set[r] = true
		}
	}
	return set
}

func gramSet(text string, width int) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(text) {
		runes := []rune(word)
		for i := 0; i+width <= len(runes); i++ {
			set[string(runes[i:i+width])] = true
		}
	}
	return set
}

func overlap[K comparable](a, b map[K]bool) int {
	n := 0
	for key := range a {
		if b[key] {
			n++
		}
	}
	return n
}

func contextWords(text string) map[string]bool {
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "to": true, "i": true,
		"we": true, "you": true, "me": true, "us": true, "can": true,
		"buy": true, "get": true, "need": true, "purchase": true, "please": true,
	}
	words := map[string]bool{}
	for _, word := range strings.Fields(text) {
		if len(word) >= 3 && !stop[word] {
			words[word] = true
		}
	}
	return words
}

func purchaseCallout(text string) bool {
	for _, word := range strings.Fields(text) {
		if word == "buy" || word == "get" || word == "need" || word == "purchase" {
			return true
		}
	}
	return false
}

// candidateHints are suggestions, never placeholders or forced replacements.
// A shared multi-character fragment is required so single characters such as
// 甲 do not bring in unrelated items.
func (l *Lexicon) candidateHints(text, target string) []string {
	const maxCandidates = 8
	const maxBudget = 520
	query := candidateText(text, target)
	width := 2
	if target == "zh" {
		width = 3
	}
	queryGrams := gramSet(query, width)
	if len(queryGrams) == 0 {
		return []string{}
	}
	queryChars := characterSet(query)
	queryWords := map[string]bool{}
	purchase := false
	if target == "zh" {
		queryWords = contextWords(query)
		purchase = purchaseCallout(query)
	}
	best := map[string]rankedTerm{}
	entries := map[string]term{}
	priority := map[string]int{}
	// Reuse the same source priority as exact lookup. Glossary rows fill gaps in
	// the plain dictionaries, including their alternative names and aliases.
	for rank, book := range []dictionary{l.custom, l.user, l.game, l.learned} {
		for source, label := range book[target] {
			if label == "" {
				continue
			}
			if rank >= 2 && l.ambiguous[source] {
				continue
			}
			if _, exists := entries[source]; exists {
				continue
			}
			entry := term{match: source, en: source, zh: label}
			if target == "en" {
				entry.en, entry.zh = label, source
			}
			if rank == 2 {
				entry.id = l.base[source].id
			}
			entries[source] = entry
			priority[source] = rank
		}
	}
	for _, terms := range l.matcher {
		for _, entry := range terms {
			if entry.en != "" && entry.zh != "" {
				if _, exists := entries[entry.match]; !exists {
					entries[entry.match] = entry
					priority[entry.match] = 2
				}
			}
		}
	}
	for _, entry := range entries {
		source := candidateText(entry.match, target)
		candidateGrams := gramSet(source, width)
		if len(candidateGrams) == 0 {
			continue
		}
		shared := overlap(candidateGrams, queryGrams)
		if shared == 0 {
			continue
		}
		chars := characterSet(source)
		score := 0.6*float64(shared)/float64(len(candidateGrams)) +
			0.4*float64(overlap(chars, queryChars))/float64(len(chars))
		if len(queryWords) > 0 {
			common := overlap(contextWords(source), queryWords)
			if purchase && common == 0 {
				continue
			}
			if common > 0 {
				score = max(score, 0.65*float64(common)/float64(len(queryWords)))
				if purchase && priority[entry.match] <= 2 {
					score += 0.45
				}
			}
		}
		if score < 0.4 {
			continue
		}
		label := entry.en
		if target == "zh" {
			label = entry.zh
		}
		line := entry.match + " = " + label
		if object, ok := l.entries[entry.id]; ok {
			line = entry.match + " = [" + object.Category + "] " + object.En + " / " + object.Zh
		}
		if settings.Size(line) > 100 {
			continue
		}
		key := normalize(label)
		if prev, ok := best[key]; !ok || score > prev.score || score == prev.score && line < prev.line {
			best[key] = rankedTerm{line: line, score: score}
		}
	}
	ranked := make([]rankedTerm, 0, len(best))
	for _, entry := range best {
		ranked = append(ranked, entry)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].line < ranked[j].line
	})
	hints := make([]string, 0, maxCandidates)
	budget := maxBudget
	for _, entry := range ranked {
		if len(hints) == maxCandidates {
			break
		}
		n := settings.Size(entry.line)
		if n > budget {
			continue
		}
		hints = append(hints, entry.line)
		budget -= n + 1
	}
	return hints
}
