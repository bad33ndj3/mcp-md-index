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
