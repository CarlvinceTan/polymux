package search

import (
	"strings"
	"unicode"
)

// Query builds an escaped FTS5 disjunction from up to 32 words, with a final prefix match.
func Query(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 32 {
		words = words[:32]
	}
	for n := range words {
		words[n] = "\"" + strings.ReplaceAll(words[n], "\"", "\"\"") + "\""
		if n == len(words)-1 {
			words[n] += "*"
		}
	}
	return strings.Join(words, " OR ")
}
