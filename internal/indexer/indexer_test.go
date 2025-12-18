package indexer

import (
	"errors"
	"testing"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
)

// --- Mock implementations for testing ---

// mockCache is a simple in-memory cache for testing.
type mockCache struct {
	mem  map[string]*domain.Index
	disk map[string]*domain.Index
}

func newMockCache() *mockCache {
	return &mockCache{
		mem:  make(map[string]*domain.Index),
		disk: make(map[string]*domain.Index),
	}
}

func (m *mockCache) Get(docID string) (*domain.Index, error) {
	if idx, ok := m.mem[docID]; ok {
		return idx, nil
	}
	return nil, errors.New("not found")
}

func (m *mockCache) Set(docID string, idx *domain.Index) {
	m.mem[docID] = idx
}

func (m *mockCache) LoadFromDisk(docID string) (*domain.Index, error) {
	if idx, ok := m.disk[docID]; ok {
		return idx, nil
	}
	return nil, errors.New("not found")
}

func (m *mockCache) SaveToDisk(idx *domain.Index) error {
	m.disk[idx.DocID] = idx
	return nil
}

// mockReader returns controlled file content.
type mockReader struct {
	files map[string]string // path -> content
}

func newMockReader() *mockReader {
	return &mockReader{files: make(map[string]string)}
}

func (m *mockReader) ReadFile(path string) ([]byte, error) {
	if content, ok := m.files[path]; ok {
		return []byte(content), nil
	}
	return nil, errors.New("file not found")
}

func (m *mockReader) HashFile(path string) (string, error) {
	if content, ok := m.files[path]; ok {
		return "hash_" + content[:min(10, len(content))], nil
	}
	return "", errors.New("file not found")
}

// mockParser returns a single chunk for any content.
type mockParser struct{}

func (mockParser) Parse(path, content string) ([]domain.Chunk, map[string]int) {
	chunks := []domain.Chunk{
		{
			ChunkID: "mock:1-10",
			DocID:   "mockdoc",
			Path:    path,
			Title:   "Mock Section",
			Text:    content,
			Terms:   []string{"mock", "test"},
		},
	}
	return chunks, map[string]int{"mock": 1, "test": 1}
}

// mockSearcher returns fixed content.
type mockSearcher struct{}

func (mockSearcher) Search(idx *domain.Index, query string, maxTokens int) string {
	return "Mock search result for: " + query
}

// mockClock returns a fixed time.
type mockClock struct {
	t time.Time
}

func (m mockClock) Now() time.Time { return m.t }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Tests ---

func TestLoad_IndexesNewFile(t *testing.T) {
	cache := newMockCache()
	reader := newMockReader()
	reader.files["docs/test.md"] = "# Hello\n\nWorld"

	indexer := New(cache, mockParser{}, mockSearcher{}, reader, mockClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})

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
	cache := newMockCache()
	reader := newMockReader()
	reader.files["docs/test.md"] = "# Hello\n\nWorld"

	indexer := New(cache, mockParser{}, mockSearcher{}, reader, mockClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})

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
	cache := newMockCache()
	reader := newMockReader()
	reader.files["docs/test.md"] = "# Original"

	indexer := New(cache, mockParser{}, mockSearcher{}, reader, mockClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})

	// First load
	_, err := indexer.Load("docs/test.md")
	if err != nil {
		t.Fatalf("First Load: %v", err)
	}

	// Clear memory cache to simulate restart
	cache.mem = make(map[string]*domain.Index)

	// Change file content (hash will change)
	reader.files["docs/test.md"] = "# Modified content"

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
	indexer := New(newMockCache(), mockParser{}, mockSearcher{}, newMockReader(), mockClock{})

	_, err := indexer.Load("")
	if err == nil {
		t.Error("Expected error for empty path")
	}
}

func TestQuery_ReturnsExcerpts(t *testing.T) {
	cache := newMockCache()
	reader := newMockReader()
	reader.files["docs/test.md"] = "# Test\n\nContent here"

	indexer := New(cache, mockParser{}, mockSearcher{}, reader, mockClock{})

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
	indexer := New(newMockCache(), mockParser{}, mockSearcher{}, newMockReader(), mockClock{})

	_, err := indexer.Query("", "docs/nonexistent.md", "test", 500)
	if err == nil {
		t.Error("Expected error for document not loaded")
	}
}

func TestQuery_ErrorsWithoutPrompt(t *testing.T) {
	cache := newMockCache()
	reader := newMockReader()
	reader.files["docs/test.md"] = "# Test"

	indexer := New(cache, mockParser{}, mockSearcher{}, reader, mockClock{})
	_, _ = indexer.Load("docs/test.md")

	_, err := indexer.Query("", "docs/test.md", "", 500) // Empty prompt
	if err == nil {
		t.Error("Expected error for empty prompt")
	}
}

func TestQuery_ErrorsWithoutDocIDOrPath(t *testing.T) {
	indexer := New(newMockCache(), mockParser{}, mockSearcher{}, newMockReader(), mockClock{})

	_, err := indexer.Query("", "", "test", 500) // Both empty
	if err == nil {
		t.Error("Expected error when both doc_id and path are empty")
	}
}
