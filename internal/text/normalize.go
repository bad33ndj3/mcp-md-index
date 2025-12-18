// Package text provides shared text processing utilities.
// This avoids duplication between parser and search packages.
package text

import (
	"regexp"
	"strings"
)

// tokenRe matches alphanumeric words.
var tokenRe = regexp.MustCompile(`[a-zA-Z0-9_]+`)

// htmlTagRe matches HTML tags like <a>, </p>, <div class="foo">
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// htmlEntityRe matches HTML entities like &amp; &#39;
var htmlEntityRe = regexp.MustCompile(`&[a-zA-Z0-9#]+;`)

// Stopwords are common words filtered during term extraction.
// These don't help distinguish between chunks.
var Stopwords = map[string]struct{}{
	// Articles and prepositions
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "to": {},
	"of": {}, "in": {}, "for": {}, "with": {}, "on": {}, "at": {},
	"by": {}, "from": {}, "as": {}, "into": {}, "through": {},
	// Common verbs
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {},
	"have": {}, "has": {}, "had": {}, "do": {}, "does": {}, "did": {},
	"will": {}, "would": {}, "could": {}, "should": {}, "may": {},
	"can": {}, "must": {},
	// Pronouns
	"it": {}, "its": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"which": {}, "what": {}, "who": {}, "whom": {},
	// Common doc words (appear everywhere, no discriminative power)
	"example": {}, "following": {}, "using": {}, "also": {},
	"when": {}, "where": {}, "how": {}, "why": {},
	"see": {}, "note": {}, "use": {}, "used": {},
	// Misc
	"over": {}, "about": {}, "above": {}, "below": {},
	// Table headers (common in API docs)
	"field": {}, "type": {}, "label": {}, "description": {},
	// Proto/gRPC doc terms (found in actual docs)
	"string": {}, "int": {}, "bool": {}, "float": {}, "uint": {},
	"optional": {}, "required": {}, "repeated": {},
	"api": {}, "svc": {}, "proto": {},
	// Common in generated docs
	"top": {}, "table": {}, "contents": {}, "value": {}, "types": {},
}

// StripHTML removes HTML tags and entities from text.
// Converts "<a href='x'>link</a> &amp; more" to "link & more"
func StripHTML(text string) string {
	// Remove HTML tags
	text = htmlTagRe.ReplaceAllString(text, "")

	// Replace common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Remove any remaining entities
	text = htmlEntityRe.ReplaceAllString(text, "")

	return text
}

// NormalizeTerms converts text into a list of searchable terms.
// It strips HTML, lowercases, tokenizes, removes stopwords, and skips short tokens.
//
// Example: "The Consumer is configured" → ["consumer", "configured"]
func NormalizeTerms(text string) []string {
	// Strip HTML first
	text = StripHTML(text)
	text = strings.ToLower(text)
	raw := tokenRe.FindAllString(text, -1)

	out := make([]string, 0, len(raw))
	for _, t := range raw {
		// Skip very short tokens
		if len(t) <= 1 {
			continue
		}
		// Skip stopwords
		if _, isStopword := Stopwords[t]; isStopword {
			continue
		}
		out = append(out, t)
	}
	return out
}
