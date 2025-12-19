// Package search implements BM25-based full-text search over indexed chunks.
// BM25 is a battle-tested ranking algorithm used by Elasticsearch, Lucene, etc.
package search

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/text"
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

	// CodeBoost gives extra weight to chunks containing code (default: 1.2)
	CodeBoost float64
}

// NewBM25Searcher creates a searcher with standard BM25 parameters.
func NewBM25Searcher() *BM25Searcher {
	return &BM25Searcher{
		K1:        1.2,
		B:         0.75,
		CodeBoost: 1.2,
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
	var sb strings.Builder

	// Add heading with breadcrumb if available
	if len(c.HeadingPath) > 1 {
		// Show parent context: "Parent > Title"
		sb.WriteString("### ")
		sb.WriteString(strings.Join(c.HeadingPath[len(c.HeadingPath)-2:], " › "))
		sb.WriteString("\n")
	} else {
		sb.WriteString("### ")
		sb.WriteString(c.Title)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Source: %s#L%d-L%d\n\n", c.Path, c.StartLine, c.EndLine))
	sb.WriteString(c.Text)
	sb.WriteString("\n")

	return sb.String()
}

// scoreChunks ranks all chunks against the query using BM25.
func (s *BM25Searcher) scoreChunks(idx *domain.Index, query string) []scoredChunk {
	qTerms := text.NormalizeTerms(query) // Use shared package
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
	codeBoost := s.CodeBoost
	if k1 == 0 {
		k1 = 1.2
	}
	if b == 0 {
		b = 0.75
	}
	if codeBoost == 0 {
		codeBoost = 1.2
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

		// Boost chunks with code blocks (often what users are looking for)
		if c.HasCode && score > 0 {
			score *= codeBoost
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

	scored := s.scoreChunks(idx, query)
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
