package indexer

import (
	"testing"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/testutil"
)

// --- Tests ---

func TestLoad_IndexesNewFile(t *testing.T) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Hello\n\nWorld"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)

	result, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if result.FromCache {
		t.Error("Expected FromCache=false for new file")
	}
	if result.NumChunks != 1 {
		t.Errorf("NumChunks = %d, want 1", result.NumChunks)
	}
	if result.DocID == "" {
		t.Error("DocID should not be empty")
	}
}

func TestLoad_UsesCacheWhenFresh(t *testing.T) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Hello\n\nWorld"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)

	// First load
	result1, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("First Load: %v", err)
	}

	// Second load should be from cache
	result2, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("Second Load: %v", err)
	}

	if !result2.FromCache {
		t.Error("Expected FromCache=true for second load")
	}
	if result2.DocID != result1.DocID {
		t.Error("DocID should be same for both loads")
	}
}

func TestLoad_ReindexesWhenFileChanged(t *testing.T) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Original"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)

	// First load
	_, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("First Load: %v", err)
	}

	// Clear memory cache to simulate restart
	cache.Mem = make(map[string]*domain.Index)

	// Change file content (hash will change)
	reader.Files["docs/test.md"] = "# Modified content"

	// Second load should re-index
	result, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("Second Load: %v", err)
	}

	if result.FromCache {
		t.Error("Expected FromCache=false after file change")
	}
}

func TestLoad_ErrorOnEmptyPath(t *testing.T) {
	indexer := New(testutil.NewMockCache(), testutil.MockParser{}, testutil.MockSearcher{}, testutil.NewMockReader(), testutil.NewMockClock(time.Time{}), nil)

	_, err := indexer.Load("")
	if err == nil {
		t.Error("Expected error for empty path")
	}
}

func TestQuery_ReturnsExcerpts(t *testing.T) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Test\n\nContent here"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)

	// Load first
	_, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Query
	result, err := indexer.Query("", "docs/test.md", "test query", 500)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}
}

func TestQuery_ErrorsWhenNotLoaded(t *testing.T) {
	indexer := New(testutil.NewMockCache(), testutil.MockParser{}, testutil.MockSearcher{}, testutil.NewMockReader(), testutil.NewMockClock(time.Time{}), nil)

	_, err := indexer.Query("", "docs/nonexistent.md", "test", 500)
	if err == nil {
		t.Error("Expected error for document not loaded")
	}
}

func TestQuery_ErrorsWithoutPrompt(t *testing.T) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Test"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)
	_, _ = indexer.Load("docs/test.md")

	_, err := indexer.Query("", "docs/test.md", "", 500) // Empty prompt
	if err == nil {
		t.Error("Expected error for empty prompt")
	}
}

func TestQuery_ErrorsWithoutDocIDOrPath(t *testing.T) {
	indexer := New(testutil.NewMockCache(), testutil.MockParser{}, testutil.MockSearcher{}, testutil.NewMockReader(), testutil.NewMockClock(time.Time{}), nil)

	_, err := indexer.Query("", "", "test", 500) // Both empty
	if err == nil {
		t.Error("Expected error when both doc_id and path are empty")
	}
}

// --- Benchmarks ---

// BenchmarkLoad measures single file loading performance.
func BenchmarkLoad(b *testing.B) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = `# Test Document

This is a test document with multiple sections.

## Section One

Content for section one with some text.

## Section Two

Content for section two with more text.
`

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear cache to force re-indexing each iteration
		cache.Mem = make(map[string]*domain.Index)
		cache.Disk = make(map[string]*domain.Index)
		_, _ = indexer.Load("docs/test.md")
	}
}

// BenchmarkLoad_FromCache measures cache hit performance.
func BenchmarkLoad_FromCache(b *testing.B) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Test\n\nContent"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)

	// Pre-load to cache
	_, _ = indexer.Load("docs/test.md")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = indexer.Load("docs/test.md")
	}
}

// BenchmarkQuery measures single document query performance.
func BenchmarkQuery(b *testing.B) {
	cache := testutil.NewMockCache()
	reader := testutil.NewMockReader()
	reader.Files["docs/test.md"] = "# Test\n\nContent about consumers and configuration"

	indexer := New(cache, testutil.MockParser{}, testutil.MockSearcher{}, reader, testutil.NewMockClock(time.Time{}), nil)
	_, _ = indexer.Load("docs/test.md")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = indexer.Query("", "docs/test.md", "consumer configuration", 500)
	}
}
