// Package testutil provides shared test helpers and mock implementations.
// This avoids duplicating mock code across test files.
package testutil

import (
	"errors"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
)

// ErrNotFound is returned by mocks when a resource doesn't exist.
var ErrNotFound = errors.New("not found")

// MockCache is a simple in-memory cache for testing.
// It separates memory and disk caches to test caching behavior.
type MockCache struct {
	Mem  map[string]*domain.Index
	Disk map[string]*domain.Index
}

// NewMockCache creates a new MockCache with initialized maps.
func NewMockCache() *MockCache {
	return &MockCache{
		Mem:  make(map[string]*domain.Index),
		Disk: make(map[string]*domain.Index),
	}
}

func (m *MockCache) Get(docID string) (*domain.Index, error) {
	if idx, ok := m.Mem[docID]; ok {
		return idx, nil
	}
	return nil, ErrNotFound
}

func (m *MockCache) Set(docID string, idx *domain.Index) {
	m.Mem[docID] = idx
}

func (m *MockCache) LoadFromDisk(docID string) (*domain.Index, error) {
	if idx, ok := m.Disk[docID]; ok {
		return idx, nil
	}
	return nil, ErrNotFound
}

func (m *MockCache) SaveToDisk(idx *domain.Index) error {
	m.Disk[idx.DocID] = idx
	return nil
}

func (m *MockCache) MarkdownPath(docID string) string {
	return "/mock/cache/" + docID + ".md"
}

func (m *MockCache) SaveMarkdown(docID string, content string) (string, error) {
	return m.MarkdownPath(docID), nil
}

func (m *MockCache) List() []string {
	docIDs := make([]string, 0, len(m.Mem))
	for docID := range m.Mem {
		docIDs = append(docIDs, docID)
	}
	return docIDs
}

// MockReader returns controlled file content for testing.
type MockReader struct {
	Files map[string]string // path -> content
}

// NewMockReader creates a MockReader with an initialized file map.
func NewMockReader() *MockReader {
	return &MockReader{Files: make(map[string]string)}
}

func (m *MockReader) ReadFile(path string) ([]byte, error) {
	if content, ok := m.Files[path]; ok {
		return []byte(content), nil
	}
	return nil, ErrNotFound
}

func (m *MockReader) HashFile(path string) (string, error) {
	if content, ok := m.Files[path]; ok {
		return "hash_" + content[:min(10, len(content))], nil
	}
	return "", ErrNotFound
}

// MockParser returns a single chunk for any content.
// Useful for testing indexer behavior without real parsing.
type MockParser struct{}

func (MockParser) Parse(path, content string) ([]domain.Chunk, map[string]int) {
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

// MockSearcher returns fixed content for testing.
type MockSearcher struct{}

func (MockSearcher) Search(idx *domain.Index, query string, maxTokens int) string {
	return "Mock search result for: " + query
}

// MockClock returns a fixed time for reproducible tests.
type MockClock struct {
	Time time.Time
}

// NewMockClock creates a clock fixed at the given time.
// If t is zero, uses 2024-01-01 00:00:00 UTC.
func NewMockClock(t time.Time) MockClock {
	if t.IsZero() {
		t = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return MockClock{Time: t}
}

func (m MockClock) Now() time.Time { return m.Time }
