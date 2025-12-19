// Package text provides shared text processing utilities for normalizing
// and tokenizing text for search indexing. Used by parser and search packages.
package text

import (
	"regexp"
	"strings"
)

// MinTokenLength is the minimum character count for a token to be indexed.
// Single-character tokens like "a", "I", "1" add noise without search value.
const MinTokenLength = 2

// tokenRe matches alphanumeric words (letters, digits, underscores).
var tokenRe = regexp.MustCompile(`[a-zA-Z0-9_]+`)

// htmlTagRe matches HTML tags like <a>, </p>, <div class="foo">
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// htmlEntityRe matches HTML entities like &amp; &#39;
var htmlEntityRe = regexp.MustCompile(`&[a-zA-Z0-9#]+;`)

// htmlEntityReplacer decodes common HTML entities to their characters.
// Using a Replacer is more efficient than repeated ReplaceAll calls.
var htmlEntityReplacer = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
	"&nbsp;", " ",
)

// stopwords contains common words filtered during term extraction.
// These appear frequently but don't help distinguish between chunks.
// Use IsStopword() to check if a term is a stopword.
var stopwords = map[string]struct{}{
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

// IsStopword returns true if the term is a common word that should be
// filtered from search indexes. Stopwords like "the", "and", "is" appear
// frequently but don't help users find specific content.
func IsStopword(term string) bool {
	_, ok := stopwords[term]
	return ok
}

// StripHTML removes HTML tags and entities from text.
// Example: "<a href='x'>link</a> &amp; more" → "link & more"
func StripHTML(text string) string {
	// Remove HTML tags first
	text = htmlTagRe.ReplaceAllString(text, "")

	// Decode common HTML entities
	text = htmlEntityReplacer.Replace(text)

	// Remove any remaining/unknown entities (e.g., &mdash;)
	text = htmlEntityRe.ReplaceAllString(text, "")

	return text
}

// NormalizeTerms converts text into a list of searchable terms.
// Processing pipeline:
//  1. Strip HTML tags and decode entities
//  2. Lowercase everything
//  3. Tokenize into alphanumeric words
//  4. Filter out short tokens (< MinTokenLength chars)
//  5. Filter out stopwords
//
// Example: "The Consumer is configured" → ["consumer", "configured"]
func NormalizeTerms(text string) []string {
	// Strip HTML first to avoid indexing tag content
	text = StripHTML(text)
	text = strings.ToLower(text)
	raw := tokenRe.FindAllString(text, -1)

	out := make([]string, 0, len(raw))
	for _, t := range raw {
		// Skip tokens shorter than minimum length (single chars add noise)
		if len(t) < MinTokenLength {
			continue
		}
		// Skip stopwords (common words with no search value)
		if IsStopword(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}
