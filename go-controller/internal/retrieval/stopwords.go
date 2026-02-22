package retrieval

import (
	"strings"
	"unicode"
)

// #region stopwords
// stopwords contains common English words excluded from topic matching.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "do": true, "does": true, "did": true,
	"have": true, "has": true, "had": true, "be": true, "been": true,
	"being": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "can": true, "shall": true, "not": true,
	"no": true, "and": true, "or": true, "but": true, "if": true,
	"then": true, "than": true, "so": true, "as": true, "at": true,
	"by": true, "for": true, "from": true, "in": true, "into": true,
	"of": true, "on": true, "to": true, "with": true, "about": true,
	"up": true, "out": true, "it": true, "its": true, "this": true,
	"that": true, "what": true, "which": true, "who": true, "how": true,
	"when": true, "where": true, "why": true, "you": true, "me": true,
	"i": true, "my": true, "your": true, "we": true, "they": true,
	"he": true, "she": true, "her": true, "him": true, "us": true,
	"them": true, "tell": true,
}

// tokenize splits text into unique lowercase non-stopword tokens.
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	seen := make(map[string]bool)
	var tokens []string
	for _, w := range words {
		if len(w) < 2 || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		tokens = append(tokens, w)
	}
	return tokens
}

// sharedKeywords returns the count of tokens present in both slices.
func sharedKeywords(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, t := range a {
		set[t] = true
	}
	count := 0
	for _, t := range b {
		if set[t] {
			count++
		}
	}
	return count
}

// #endregion stopwords
