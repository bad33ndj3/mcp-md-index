package parser

import (
	"strings"
	"testing"
)

// NOTE: NormalizeTerms tests removed - they duplicate text/normalize_test.go.
// The parser.NormalizeTerms wrapper just delegates to text.NormalizeTerms.

func TestParse_SplitsByHeadings(t *testing.T) {
	parser := &MarkdownParser{
		MaxLinesPerChunk: 120,
		MinLinesPerChunk: 3, // Lower threshold for test
	}

	content := `# Main Title

Some intro text here.
More content.
Even more.

## First Section

Content of first section with multiple
lines of text here.
And more text.

## Second Section

Content of second section.
More content here.
Additional lines.
`

	chunks, docFreq := parser.Parse("test.md", content)

	// Should create multiple chunks based on headings
	if len(chunks) < 2 {
		t.Errorf("Expected at least 2 chunks, got %d", len(chunks))
		for i, c := range chunks {
			t.Logf("Chunk %d: title=%q, lines=%d-%d", i, c.Title, c.StartLine, c.EndLine)
		}
	}

	// Verify chunk titles exist
	titles := make([]string, len(chunks))
	for i, c := range chunks {
		titles[i] = c.Title
	}
	t.Logf("Chunk titles: %v", titles)

	// docFreq should track term occurrence
	if len(docFreq) == 0 {
		t.Error("Expected docFreq to have entries")
	}
}

func TestParse_RespectMaxLines(t *testing.T) {
	parser := &MarkdownParser{
		MaxLinesPerChunk: 5,
		MinLinesPerChunk: 2,
	}

	// Create content with no headings but many lines
	var lines []string
	for i := range 20 {
		lines = append(lines, "This is line number "+string(rune('A'+i)))
	}
	content := strings.Join(lines, "\n")

	chunks, _ := parser.Parse("test.md", content)

	// With max 5 lines per chunk, 20 lines should create at least 4 chunks
	if len(chunks) < 4 {
		t.Errorf("Expected at least 4 chunks (20 lines / 5 max), got %d", len(chunks))
	}

	// Verify no chunk exceeds max lines
	for i, c := range chunks {
		lineCount := c.EndLine - c.StartLine + 1
		if lineCount > parser.MaxLinesPerChunk {
			t.Errorf("Chunk %d has %d lines, exceeds max %d", i, lineCount, parser.MaxLinesPerChunk)
		}
	}
}

func TestParse_ChunkHasCorrectMetadata(t *testing.T) {
	parser := NewMarkdownParser()

	content := `# Test Document

This is the content.
`

	chunks, _ := parser.Parse("/path/to/doc.md", content)

	if len(chunks) == 0 {
		t.Fatal("Expected at least 1 chunk")
	}

	c := chunks[0]

	// Verify DocID is set
	if c.DocID == "" {
		t.Error("DocID should not be empty")
	}

	// Verify ChunkID format includes line range
	if !strings.Contains(c.ChunkID, ":") {
		t.Errorf("ChunkID %q should contain line range", c.ChunkID)
	}

	// Verify Path is preserved
	if c.Path != "/path/to/doc.md" {
		t.Errorf("Path = %q, want /path/to/doc.md", c.Path)
	}

	// Verify Terms are extracted
	if len(c.Terms) == 0 {
		t.Error("Terms should not be empty")
	}
}

func TestDocIDForPath_Deterministic(t *testing.T) {
	// Same path should always produce same ID
	id1 := DocIDForPath("docs/nats.md")
	id2 := DocIDForPath("docs/nats.md")

	if id1 != id2 {
		t.Errorf("DocIDForPath not deterministic: %q != %q", id1, id2)
	}

	// Different paths should produce different IDs
	id3 := DocIDForPath("docs/other.md")
	if id1 == id3 {
		t.Errorf("Different paths produced same ID: %q", id1)
	}

	// ID should be 16 chars (SHA256 prefix)
	if len(id1) != 16 {
		t.Errorf("DocID length = %d, want 16", len(id1))
	}
}

// --- Benchmarks ---

// BenchmarkParse_SmallDoc measures parsing of a small markdown document.
func BenchmarkParse_SmallDoc(b *testing.B) {
	parser := NewMarkdownParser()
	content := `# Test Document

This is a simple test document with some content.

## Section One

Content for section one with regular text.

## Section Two

Content for section two with more text.
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse("test.md", content)
	}
}

// BenchmarkParse_LargeDoc measures parsing of a larger document with code blocks.
func BenchmarkParse_LargeDoc(b *testing.B) {
	parser := NewMarkdownParser()

	// Build a larger document
	var sb strings.Builder
	sb.WriteString("# Large Test Document\n\n")
	sb.WriteString("Introduction text for the document.\n\n")

	for i := 0; i < 20; i++ {
		sb.WriteString("## Section " + string(rune('A'+i)) + "\n\n")
		sb.WriteString("This section contains information about topic " + string(rune('A'+i)) + ".\n")
		sb.WriteString("It has multiple lines of content that describe the topic in detail.\n")
		sb.WriteString("Additional context and examples are provided below.\n\n")
		sb.WriteString("```go\n")
		sb.WriteString("func example" + string(rune('A'+i)) + "() {\n")
		sb.WriteString("    fmt.Println(\"Hello from section " + string(rune('A'+i)) + "\")\n")
		sb.WriteString("}\n")
		sb.WriteString("```\n\n")
	}

	content := sb.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse("large.md", content)
	}
}

// BenchmarkParse_WithTables measures parsing with markdown tables.
func BenchmarkParse_WithTables(b *testing.B) {
	parser := NewMarkdownParser()
	content := `# API Reference

## Fields

| Field | Type | Description |
|-------|------|-------------|
| name | string | The name of the resource |
| id | int | Unique identifier |
| enabled | bool | Whether the feature is enabled |
| config | object | Configuration options |

## Methods

| Method | Parameters | Returns |
|--------|------------|---------|
| Create | name, config | Resource |
| Update | id, config | Resource |
| Delete | id | void |
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parser.Parse("api.md", content)
	}
}

// BenchmarkDocIDForPath measures document ID generation.
func BenchmarkDocIDForPath(b *testing.B) {
	paths := []string{
		"docs/api.md",
		"internal/service/handler.go",
		"/absolute/path/to/file.md",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range paths {
			_ = DocIDForPath(p)
		}
	}
}
