package terminal

import (
	"slices"
	"strings"
	"unicode"
)

// Match is one candidate that matched a fuzzy query: its text, where the
// query's letters landed in it (rune indexes) and how good the match is.
type Match struct {
	Text      string
	Positions []int
	Score     int
}

// FuzzyFind returns the candidates that contain query's letters in order,
// best first. Matching ignores case unless the query has an uppercase
// letter. Letters that follow each other, and letters at the start of a word,
// score higher; ties keep the candidates' order, so pass the newest first.
// An empty query matches everything, in order.
func FuzzyFind(query string, candidates []string) []Match {
	q := []rune(query)
	caseSensitive := strings.IndexFunc(query, unicode.IsUpper) >= 0
	var out []Match
	for _, c := range candidates {
		if m, ok := fuzzyMatch(q, c, caseSensitive); ok {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b Match) int { return b.Score - a.Score })
	return out
}

func fuzzyMatch(q []rune, text string, caseSensitive bool) (Match, bool) {
	m := Match{Text: text}
	if len(q) == 0 {
		return m, true
	}
	runes := []rune(text)
	fold := func(r rune) rune {
		if caseSensitive {
			return r
		}
		return unicode.ToLower(r)
	}
	// Greedy left-to-right match, then scored. A word start is the first
	// rune, or one after a space or punctuation like / - _ .
	qi, prev := 0, -2
	for i, r := range runes {
		if qi == len(q) {
			break
		}
		if fold(r) != fold(q[qi]) {
			continue
		}
		m.Positions = append(m.Positions, i)
		m.Score += 1
		if i == prev+1 {
			m.Score += 5 // consecutive
		}
		if i == 0 || strings.ContainsRune(" /-_.:", runes[i-1]) {
			m.Score += 3 // start of a word
		}
		prev = i
		qi++
	}
	if qi < len(q) {
		return Match{}, false
	}
	m.Score -= len(runes) / 10 // shorter entries win close calls
	return m, true
}
