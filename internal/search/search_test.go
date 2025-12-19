package search

import (
	"strings"
	"testing"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
)

func TestSearch_RanksRelevantFirst(t *testing.T) {
	// Create an index with chunks about different topics
	idx := &domain.Index{
		DocID: "test123",
		Path:  "test.md",
		Chunks: []domain.Chunk{
			{
				ChunkID: "1", Title: "Introduction", Text: "Welcome to our guide.",
				Terms: []string{"welcome", "guide"},
			},
			{
				ChunkID: "2", Title: "Consumer Config", Text: "The consumer is configured with these options. Consumer settings are important.",
				Terms: []string{"consumer", "configured", "options", "consumer", "settings", "important"},
			},
			{
				ChunkID: "3", Title: "Producer Setup", Text: "The producer sends messages.",
				Terms: []string{"producer", "sends", "messages"},
			},
		},
		DocFreq:   map[string]int{"welcome": 1, "guide": 1, "consumer": 1, "configured": 1, "options": 1, "settings": 1, "important": 1, "producer": 1, "sends": 1, "messages": 1},
		NumChunks: 3,
		Version:   domain.CacheVersion,
	}

	searcher := NewBM25Searcher()
	result := searcher.Search(idx, "consumer configuration", 1000)

	// The consumer chunk should appear first (or be the primary result)
	if !strings.Contains(result, "Consumer Config") {
		t.Errorf("Expected 'Consumer Config' in result, got: %s", result)
	}

	// Check that source link is included
	if !strings.Contains(result, "Source:") {
		t.Errorf("Expected 'Source:' link in result")
	}
}

func TestSearch_RespectsTokenLimit(t *testing.T) {
	// Create an index with chunks that would exceed token limit
	idx := &domain.Index{
		DocID: "test123",
		Path:  "test.md",
		Chunks: []domain.Chunk{
			{
				ChunkID: "1", Title: "First", Text: strings.Repeat("keyword word ", 100),
				Terms: []string{"keyword", "word"},
			},
			{
				ChunkID: "2", Title: "Second", Text: strings.Repeat("keyword another ", 100),
				Terms: []string{"keyword", "another"},
			},
		},
		DocFreq:   map[string]int{"keyword": 2, "word": 1, "another": 1},
		NumChunks: 2,
		Version:   domain.CacheVersion,
	}

	searcher := NewBM25Searcher()

	// With a very low token limit, output should be bounded
	result := searcher.Search(idx, "keyword", 50)
	tokens := approxTokens(result)

	// Allow some overhead for formatting, but should be reasonably close
	if tokens > 100 { // Some slack for formatting
		t.Errorf("Token limit not respected: got ~%d tokens, wanted ~50", tokens)
	}
}

func TestSearch_NoResults(t *testing.T) {
	idx := &domain.Index{
		DocID: "test123",
		Path:  "test.md",
		Chunks: []domain.Chunk{
			{ChunkID: "1", Title: "First", Text: "Hello world", Terms: []string{"hello", "world"}},
		},
		DocFreq:   map[string]int{"hello": 1, "world": 1},
		NumChunks: 1,
		Version:   domain.CacheVersion,
	}

	searcher := NewBM25Searcher()

	// Search for term not in the document
	result := searcher.Search(idx, "xyznonexistent", 500)

	if !strings.Contains(result, "No relevant excerpts") {
		t.Errorf("Expected 'No relevant excerpts' message, got: %s", result)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	idx := &domain.Index{
		DocID: "test123",
		Path:  "test.md",
		Chunks: []domain.Chunk{
			{ChunkID: "1", Title: "First", Text: "Hello world", Terms: []string{"hello", "world"}},
		},
		DocFreq:   map[string]int{"hello": 1, "world": 1},
		NumChunks: 1,
		Version:   domain.CacheVersion,
	}

	searcher := NewBM25Searcher()

	// Empty query should return no results message
	result := searcher.Search(idx, "", 500)

	if !strings.Contains(result, "No relevant excerpts") {
		t.Errorf("Expected 'No relevant excerpts' for empty query, got: %s", result)
	}
}

func TestFormatExcerpt_IncludesSourceLink(t *testing.T) {
	chunk := domain.Chunk{
		ChunkID:   "abc:10-20",
		DocID:     "abc",
		Path:      "docs/test.md",
		Title:     "Test Section",
		StartLine: 10,
		EndLine:   20,
		Text:      "This is test content.",
	}

	result := formatExcerpt(chunk)

	if !strings.Contains(result, "### Test Section") {
		t.Error("Missing title heading")
	}
	if !strings.Contains(result, "Source: docs/test.md#L10-L20") {
		t.Error("Missing or incorrect source link")
	}
	if !strings.Contains(result, "This is test content.") {
		t.Error("Missing chunk text")
	}
}

func TestApproxTokens(t *testing.T) {
	tests := []struct {
		input     string
		minExpect int
		maxExpect int
	}{
		{"", 0, 1},
		{"word", 1, 2},
		{"hello world this is a test", 5, 10},
	}

	for _, tc := range tests {
		got := approxTokens(tc.input)
		if got < tc.minExpect || got > tc.maxExpect {
			t.Errorf("approxTokens(%q) = %d, expected between %d and %d",
				tc.input, got, tc.minExpect, tc.maxExpect)
		}
	}
}

// --- Benchmarks ---

// createTestIndex creates an index with n chunks for benchmarking.
func createTestIndex(n int) *domain.Index {
	chunks := make([]domain.Chunk, n)
	docFreq := make(map[string]int)

	for i := 0; i < n; i++ {
		terms := []string{"consumer", "configuration", "options", "settings"}
		if i%2 == 0 {
			terms = append(terms, "durable", "persistence")
		}
		if i%3 == 0 {
			terms = append(terms, "ephemeral", "cleanup")
		}

		chunks[i] = domain.Chunk{
			ChunkID:   strings.Repeat("a", 8) + ":" + string(rune('0'+i)) + "-" + string(rune('0'+i+10)),
			DocID:     "testdoc",
			Path:      "test.md",
			Title:     "Section " + string(rune('A'+i%26)),
			StartLine: i * 10,
			EndLine:   i*10 + 10,
			Text:      "This section covers consumer configuration options and settings for the system.",
			Terms:     terms,
			HasCode:   i%4 == 0,
		}

		for _, t := range terms {
			docFreq[t]++
		}
	}

	return &domain.Index{
		DocID:     "testdoc",
		Path:      "test.md",
		Chunks:    chunks,
		DocFreq:   docFreq,
		NumChunks: n,
		Version:   domain.CacheVersion,
	}
}

// BenchmarkScoreChunks_Small measures BM25 scoring on a small index.
func BenchmarkScoreChunks_Small(b *testing.B) {
	idx := createTestIndex(10)
	searcher := NewBM25Searcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = searcher.scoreChunks(idx, "consumer configuration")
	}
}

// BenchmarkScoreChunks_Medium measures BM25 scoring on a medium index.
func BenchmarkScoreChunks_Medium(b *testing.B) {
	idx := createTestIndex(50)
	searcher := NewBM25Searcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = searcher.scoreChunks(idx, "consumer configuration")
	}
}

// BenchmarkScoreChunks_Large measures BM25 scoring on a large index.
func BenchmarkScoreChunks_Large(b *testing.B) {
	idx := createTestIndex(200)
	searcher := NewBM25Searcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = searcher.scoreChunks(idx, "consumer configuration")
	}
}

// BenchmarkSearch_Small measures full search on a small index.
func BenchmarkSearch_Small(b *testing.B) {
	idx := createTestIndex(10)
	searcher := NewBM25Searcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = searcher.Search(idx, "consumer configuration", 500)
	}
}

// BenchmarkSearch_Large measures full search on a large index.
func BenchmarkSearch_Large(b *testing.B) {
	idx := createTestIndex(200)
	searcher := NewBM25Searcher()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = searcher.Search(idx, "consumer configuration", 500)
	}
}

// BenchmarkFormatExcerpt measures excerpt formatting.
func BenchmarkFormatExcerpt(b *testing.B) {
	chunk := domain.Chunk{
		ChunkID:     "abc123:10-20",
		DocID:       "abc123",
		Path:        "docs/test.md",
		Title:       "Consumer Configuration",
		HeadingPath: []string{"NATS Guide", "Consumers", "Consumer Configuration"},
		StartLine:   10,
		EndLine:     20,
		Text:        "This section covers consumer configuration options and settings for NATS JetStream.",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = formatExcerpt(chunk)
	}
}

// BenchmarkApproxTokens measures token approximation.
func BenchmarkApproxTokens(b *testing.B) {
	text := "This is a sample text that would be returned as an excerpt from the search results."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = approxTokens(text)
	}
}
