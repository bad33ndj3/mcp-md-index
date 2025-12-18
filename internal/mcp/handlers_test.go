package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/indexer"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Mock implementations ---

type mockCache struct {
	mem map[string]*domain.Index
}

func (m *mockCache) Get(docID string) (*domain.Index, error) {
	if idx, ok := m.mem[docID]; ok {
		return idx, nil
	}
	return nil, errors.New("not found")
}

func (m *mockCache) Set(docID string, idx *domain.Index) { m.mem[docID] = idx }
func (m *mockCache) LoadFromDisk(docID string) (*domain.Index, error) {
	return nil, errors.New("not found")
}
func (m *mockCache) SaveToDisk(idx *domain.Index) error { return nil }

type mockParser struct{}

func (mockParser) Parse(path, content string) ([]domain.Chunk, map[string]int) {
	return []domain.Chunk{{ChunkID: "1", Text: content, Terms: []string{"test"}}}, map[string]int{"test": 1}
}

type mockSearcher struct{}

func (mockSearcher) Search(idx *domain.Index, query string, maxTokens int) string {
	return "Result for: " + query
}

type mockReader struct {
	files map[string]string
}

func (m *mockReader) ReadFile(path string) ([]byte, error) {
	if c, ok := m.files[path]; ok {
		return []byte(c), nil
	}
	return nil, errors.New("not found")
}

func (m *mockReader) HashFile(path string) (string, error) {
	if _, ok := m.files[path]; ok {
		return "hash123", nil
	}
	return "", errors.New("not found")
}

type mockClock struct{}

func (mockClock) Now() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

func createTestHandlers() (*Handlers, *mockReader) {
	cache := &mockCache{mem: make(map[string]*domain.Index)}
	reader := &mockReader{files: map[string]string{"docs/test.md": "# Test\n\nContent"}}
	idx := indexer.New(cache, mockParser{}, mockSearcher{}, reader, mockClock{})
	return NewHandlers(idx), reader
}

// getTextFromResult extracts text content from MCP result
func getTextFromResult(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	// mcp.TextContent is the actual type returned
	if tc, ok := result.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// --- Tests ---

func TestDocsLoad_ReturnsDocID(t *testing.T) {
	handlers, _ := createTestHandlers()

	result, _, err := handlers.DocsLoad(context.Background(), nil, LoadArgs{Path: "docs/test.md"})
	if err != nil {
		t.Fatalf("DocsLoad: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected content in result")
	}

	text := getTextFromResult(result)
	if !strings.Contains(text, "doc_id:") {
		t.Errorf("Response should contain doc_id, got: %s", text)
	}
}

func TestDocsLoad_ErrorsOnEmptyPath(t *testing.T) {
	handlers, _ := createTestHandlers()

	_, _, err := handlers.DocsLoad(context.Background(), nil, LoadArgs{Path: ""})
	if err == nil {
		t.Error("Expected error for empty path")
	}
}

func TestDocsQuery_ReturnsExcerpts(t *testing.T) {
	handlers, _ := createTestHandlers()

	// Load first
	_, _, err := handlers.DocsLoad(context.Background(), nil, LoadArgs{Path: "docs/test.md"})
	if err != nil {
		t.Fatalf("DocsLoad: %v", err)
	}

	// Query
	result, _, err := handlers.DocsQuery(context.Background(), nil, QueryArgs{
		Path:   "docs/test.md",
		Prompt: "test query",
	})
	if err != nil {
		t.Fatalf("DocsQuery: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected content in result")
	}

	text := getTextFromResult(result)
	if !strings.Contains(text, "Result for:") {
		t.Errorf("Unexpected response: %s", text)
	}
}

func TestDocsQuery_ErrorsWithoutDocIDOrPath(t *testing.T) {
	handlers, _ := createTestHandlers()

	_, _, err := handlers.DocsQuery(context.Background(), nil, QueryArgs{
		Prompt: "test",
	})
	if err == nil {
		t.Error("Expected error when both doc_id and path are empty")
	}
}

func TestDocsQuery_ErrorsWithoutPrompt(t *testing.T) {
	handlers, _ := createTestHandlers()

	_, _, err := handlers.DocsQuery(context.Background(), nil, QueryArgs{
		Path: "docs/test.md",
	})
	if err == nil {
		t.Error("Expected error for empty prompt")
	}
}
