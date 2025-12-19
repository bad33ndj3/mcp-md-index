// Package search implements BM25-based full-text search over indexed chunks.
// BM25 is a battle-tested ranking algorithm used by Elasticsearch, Lucene, etc.
package search

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/text"
)

// ─────────────────────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────────────────────

// Searcher defines how queries are matched against indexed documents.
type Searcher interface {
	Search(idx *domain.Index, query string, maxTokens int) string
}

// BM25Config holds the tuning parameters for BM25 scoring.
type BM25Config struct {
	K1        float64 // Term frequency saturation (default: 1.2)
	B         float64 // Length normalization (default: 0.75)
	CodeBoost float64 // Extra weight for code chunks (default: 1.2)
}

// DefaultBM25Config returns standard BM25 parameters.
func DefaultBM25Config() BM25Config {
	return BM25Config{
		K1:        1.2,
		B:         0.75,
		CodeBoost: 1.2,
	}
}

// BM25Searcher uses the BM25 algorithm for ranking chunks.
type BM25Searcher struct {
	config BM25Config
}

// NewBM25Searcher creates a searcher with standard BM25 parameters.
func NewBM25Searcher() *BM25Searcher {
	return &BM25Searcher{config: DefaultBM25Config()}
}

// scoredChunk pairs a chunk with its relevance score.
type scoredChunk struct {
	chunk domain.Chunk
	score float64
}

// termFrequency counts how often each term appears.
type termFrequency map[string]int

// ─────────────────────────────────────────────────────────────────────────────
// Object Pool (reduces memory allocations)
// ─────────────────────────────────────────────────────────────────────────────

var tfPool = sync.Pool{
	New: func() any { return make(termFrequency, 32) },
}

func borrowTF() termFrequency   { return tfPool.Get().(termFrequency) }
func returnTF(tf termFrequency) { clear(tf); tfPool.Put(tf) }

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// approxTokens estimates token count (~4 bytes per token).
func approxTokens(s string) int {
	return (len(s) + 3) / 4
}

// formatExcerpt creates a markdown-formatted excerpt with source link.
func formatExcerpt(c domain.Chunk) string {
	var sb strings.Builder
	sb.Grow(len(c.Title) + len(c.Path) + len(c.Text) + 100)

	// Title with optional breadcrumb
	sb.WriteString("### ")
	if len(c.HeadingPath) > 1 {
		sb.WriteString(c.HeadingPath[len(c.HeadingPath)-2])
		sb.WriteString(" › ")
		sb.WriteString(c.HeadingPath[len(c.HeadingPath)-1])
	} else {
		sb.WriteString(c.Title)
	}
	sb.WriteByte('\n')

	// Source link
	sb.WriteString("Source: ")
	sb.WriteString(c.Path)
	sb.WriteString("#L")
	sb.WriteString(fmt.Sprint(c.StartLine))
	sb.WriteString("-L")
	sb.WriteString(fmt.Sprint(c.EndLine))
	sb.WriteString("\n\n")

	// Content
	sb.WriteString(c.Text)
	sb.WriteByte('\n')

	return sb.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// BM25 Scoring
// ─────────────────────────────────────────────────────────────────────────────

// calcIDF computes inverse document frequency for a term.
// Rare terms get higher scores (they're more discriminating).
func calcIDF(numChunks, docFreq float64) float64 {
	return math.Log(1.0 + (numChunks-docFreq+0.5)/(docFreq+0.5))
}

// calcTF computes the BM25 term frequency component.
// Includes saturation (diminishing returns) and length normalization.
func calcTF(termCount, docLen, avgLen, k1, b float64) float64 {
	denominator := termCount + k1*(1.0-b+b*(docLen/avgLen))
	if denominator == 0 {
		return 0
	}
	return (termCount * (k1 + 1.0)) / denominator
}

// scoreChunk calculates BM25 score for a single chunk against query terms.
func (s *BM25Searcher) scoreChunk(
	chunk domain.Chunk,
	queryTermCounts termFrequency,
	docFreq map[string]int,
	numChunks, avgLen float64,
) float64 {
	// Build term frequency map for this chunk
	tf := borrowTF()
	defer returnTF(tf)

	for _, term := range chunk.Terms {
		tf[term]++
	}

	docLen := float64(len(chunk.Terms))
	cfg := s.config
	score := 0.0

	// Sum BM25 contribution from each query term
	for term, queryFreq := range queryTermCounts {
		df := float64(docFreq[term])
		if df == 0 {
			continue // Term not in corpus
		}

		idf := calcIDF(numChunks, df)
		tfScore := calcTF(float64(tf[term]), docLen, avgLen, cfg.K1, cfg.B)
		score += idf * tfScore * float64(queryFreq)
	}

	// Boost chunks with code (often what users want)
	if chunk.HasCode && score > 0 {
		score *= cfg.CodeBoost
	}

	return score
}

// scoreChunks ranks all chunks against the query using BM25.
func (s *BM25Searcher) scoreChunks(idx *domain.Index, query string) []scoredChunk {
	queryTerms := text.NormalizeTerms(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// Count query term frequencies
	queryTermCounts := make(termFrequency, len(queryTerms))
	for _, t := range queryTerms {
		queryTermCounts[t]++
	}

	numChunks := float64(idx.NumChunks)
	if numChunks == 0 {
		return nil
	}

	// Calculate average chunk length (needed for length normalization)
	avgLen := 0.0
	for _, c := range idx.Chunks {
		avgLen += float64(len(c.Terms))
	}
	avgLen /= numChunks

	// Score all chunks
	results := make([]scoredChunk, 0, len(idx.Chunks))
	for _, chunk := range idx.Chunks {
		score := s.scoreChunk(chunk, queryTermCounts, idx.DocFreq, numChunks, avgLen)
		if score > 0 {
			results = append(results, scoredChunk{chunk: chunk, score: score})
		}
	}

	// Sort by score (best first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	return results
}

// ─────────────────────────────────────────────────────────────────────────────
// Search (public API)
// ─────────────────────────────────────────────────────────────────────────────

// Search returns the top-scoring excerpts that fit within the token limit.
func (s *BM25Searcher) Search(idx *domain.Index, query string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = domain.DefaultMaxTokens
	}

	scored := s.scoreChunks(idx, query)
	if len(scored) == 0 {
		return "No relevant excerpts found in the indexed document."
	}

	return s.buildResponse(scored, maxTokens)
}

// buildResponse assembles excerpts into a formatted response.
func (s *BM25Searcher) buildResponse(scored []scoredChunk, maxTokens int) string {
	var out strings.Builder
	tokensUsed := 0
	excerptCount := 0

	for _, sc := range scored {
		excerpt := formatExcerpt(sc.chunk)
		tokens := approxTokens(excerpt)

		// Trim first excerpt if too large
		if excerptCount == 0 && tokens > maxTokens {
			excerpt = s.trimExcerpt(sc.chunk, maxTokens)
			tokens = approxTokens(excerpt)
		}

		// Stop if adding this would exceed budget
		if tokensUsed+tokens > maxTokens {
			break
		}

		// Add separator between excerpts
		if excerptCount > 0 {
			out.WriteString("\n--------------------------------\n\n")
		}

		out.WriteString(excerpt)
		tokensUsed += tokens
		excerptCount++

		if tokensUsed >= maxTokens {
			break
		}
	}

	if excerptCount == 0 {
		return "Token limit too small to return any excerpt."
	}

	return out.String()
}

// trimExcerpt shortens a chunk's text to fit within token limit.
func (s *BM25Searcher) trimExcerpt(chunk domain.Chunk, maxTokens int) string {
	excerpt := formatExcerpt(chunk)
	tokens := approxTokens(excerpt)
	over := tokens - maxTokens

	runes := []rune(chunk.Text)
	cut := len(runes) - over*4

	if cut < 80 {
		cut = min(80, len(runes))
	}
	if cut < len(runes) {
		chunk.Text = string(runes[:cut]) + "\n…"
	}

	return formatExcerpt(chunk)
}
