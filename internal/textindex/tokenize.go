package textindex

import (
	"strings"
	"unicode"
)

const maxTokenLen = 64

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
	"for": {}, "from": {}, "has": {}, "in": {}, "is": {}, "it": {}, "of": {}, "on": {},
	"or": {}, "that": {}, "the": {}, "this": {}, "to": {}, "was": {}, "were": {}, "with": {},
}

// Tokenize returns normalized terms for Beacon's precomputed search index.
func Tokenize(text string) []string {
	var tokens []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		token := b.String()
		b.Reset()
		if len(token) < 2 {
			return
		}
		if _, ok := stopWords[token]; ok {
			return
		}
		tokens = append(tokens, token)
	}

	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/' || r == '.' {
			if b.Len() < maxTokenLen {
				b.WriteRune(r)
			}
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func Frequencies(tokens []string) map[string]int {
	freq := make(map[string]int, len(tokens))
	for _, token := range tokens {
		freq[token]++
	}
	return freq
}
