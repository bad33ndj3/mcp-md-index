// Package search implements BM25-based full-text search over indexed chunks.
// BM25 is a battle-tested ranking algorithm used by Elasticsearch, Lucene, etc.
package search

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
)

// Searcher defines how queries are matched against indexed documents.
type Searcher interface {
	// Search returns token-bounded, source-linked excerpts for a query.
	Search(idx *domain.Index, query string, maxTokens int) string
}

// BM25Searcher uses the BM25 algorithm for ranking chunks.
// BM25 is an improvement over TF-IDF that handles document length normalization.
type BM25Searcher struct {
	// K1 controls term frequency saturation (default: 1.2)
	// Higher values = more weight on term frequency
	K1 float64

	// B controls length normalization (default: 0.75)
	// 0 = no normalization, 1 = full normalization
	B float64
}

// NewBM25Searcher creates a searcher with standard BM25 parameters.
func NewBM25Searcher() *BM25Searcher {
	return &BM25Searcher{
		K1: 1.2,
		B:  0.75,
	}
}

// scoredChunk pairs a chunk with its relevance score.
type scoredChunk struct {
	Chunk domain.Chunk
	Score float64
}

// approxTokens estimates token count from text length.
// Uses ~4 characters per token as a rough heuristic.
func approxTokens(s string) int {
	n := len([]rune(s))
	return int(math.Ceil(float64(n) / 4.0))
}

// formatExcerpt creates a markdown-formatted excerpt with source link.
func formatExcerpt(c domain.Chunk) string {
	return fmt.Sprintf(
		"### %s\nSource: %s#L%d-L%d\n\n%s\n",
		c.Title, c.Path, c.StartLine, c.EndLine, c.Text,
	)
}

// scoreChunks ranks all chunks against the query using BM25.
func (s *BM25Searcher) scoreChunks(idx *domain.Index, query string, normalizeTerms func(string) []string) []scoredChunk {
	qTerms := normalizeTerms(query)
	if len(qTerms) == 0 {
		return nil
	}

	// Count query term frequencies
	qCounts := make(map[string]int)
	for _, t := range qTerms {
		qCounts[t]++
	}

	N := float64(idx.NumChunks)
	if N == 0 {
		return nil
	}

	// Calculate average document length
	avgLen := 0.0
	for _, c := range idx.Chunks {
		avgLen += float64(len(c.Terms))
	}
	avgLen /= N

	k1 := s.K1
	b := s.B
	if k1 == 0 {
		k1 = 1.2
	}
	if b == 0 {
		b = 0.75
	}

	// Score each chunk
	out := make([]scoredChunk, 0, len(idx.Chunks))
	for _, c := range idx.Chunks {
		// Count term frequencies in this chunk
		tf := make(map[string]int)
		for _, t := range c.Terms {
			tf[t]++
		}
		docLen := float64(len(c.Terms))

		// Calculate BM25 score
		score := 0.0
		for term, qf := range qCounts {
			df := float64(idx.DocFreq[term])
			if df == 0 {
				continue // Term not in any chunk
			}

			// IDF: log(1 + (N - df + 0.5) / (df + 0.5))
			idf := math.Log(1.0 + (N-df+0.5)/(df+0.5))

			// TF saturation with length normalization
			f := float64(tf[term])
			den := f + k1*(1.0-b+b*(docLen/avgLen))
			if den == 0 {
				continue
			}

			score += idf * ((f * (k1 + 1.0)) / den) * float64(qf)
		}

		if score > 0 {
			out = append(out, scoredChunk{Chunk: c, Score: score})
		}
	}

	// Sort by score descending
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Search returns the top-scoring excerpts that fit within the token limit.
func (s *BM25Searcher) Search(idx *domain.Index, query string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = domain.DefaultMaxTokens
	}

	scored := s.scoreChunks(idx, query, NormalizeTermsForSearch)
	if len(scored) == 0 {
		return "No relevant excerpts found in the indexed document."
	}

	var b strings.Builder
	used := 0
	added := 0

	for _, sc := range scored {
		ex := formatExcerpt(sc.Chunk)
		tok := approxTokens(ex)

		// If the best hit is bigger than limit, hard-trim chunk text
		if added == 0 && tok > maxTokens {
			trimmed := sc.Chunk
			over := tok - maxTokens
			r := []rune(trimmed.Text)
			cut := len(r) - over*4
			if cut < 80 {
				cut = min(80, len(r))
			}
			if cut < len(r) {
				trimmed.Text = string(r[:cut]) + "\n…"
			}
			ex = formatExcerpt(trimmed)
			tok = approxTokens(ex)
		}

		if used+tok > maxTokens {
			break
		}

		if added > 0 {
			b.WriteString("\n--------------------------------\n\n")
		}
		b.WriteString(ex)
		used += tok
		added++
		if used >= maxTokens {
			break
		}
	}

	if added == 0 {
		return "Token limit too small to return any excerpt."
	}
	return b.String()
}

// NormalizeTermsForSearch is a copy of parser.NormalizeTerms to avoid circular import.
// In a larger codebase, this would be in a shared util package.
func NormalizeTermsForSearch(text string) []string {
	text = strings.ToLower(text)

	// Simple token extraction
	var tokens []string
	start := -1
	for i, r := range text {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if isAlnum {
			if start == -1 {
				start = i
			}
		} else {
			if start != -1 {
				tokens = append(tokens, text[start:i])
				start = -1
			}
		}
	}
	if start != -1 {
		tokens = append(tokens, text[start:])
	}

	// Stopwords to filter
	stopwords := map[string]struct{}{
		"the": {}, "and": {}, "or": {}, "to": {}, "of": {}, "in": {},
		"a": {}, "an": {}, "for": {}, "with": {}, "on": {}, "is": {},
		"are": {}, "as": {}, "by": {}, "be": {},
	}

	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if len(t) <= 1 {
			continue
		}
		if _, isStopword := stopwords[t]; isStopword {
			continue
		}
		out = append(out, t)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
