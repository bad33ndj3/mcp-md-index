package parser

import (
	"strings"
	"testing"
)

func TestNormalizeTerms_Basic(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			input: "Hello World",
			want:  []string{"hello", "world"},
		},
		{
			input: "The consumer is configured",
			want:  []string{"consumer", "configured"},
		},
		{
			input: "Go 1.23 supports_underscores",
			want:  []string{"go", "23", "supports_underscores"},
		},
		{
			input: "a b c", // Single chars filtered
			want:  []string{},
		},
		{
			input: "", // Empty input
			want:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeTerms(tc.input)
			if len(got) != len(tc.want) {
				t.Errorf("NormalizeTerms(%q) = %v, want %v", tc.input, got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("NormalizeTerms(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeTerms_RemovesStopwords(t *testing.T) {
	input := "the quick brown fox jumps over the lazy dog"
	got := NormalizeTerms(input)

	// "the" and "over" should be filtered
	for _, term := range got {
		if term == "the" || term == "over" {
			t.Errorf("Stopword %q should have been filtered", term)
		}
	}

	// "quick", "brown", "fox", "jumps", "lazy", "dog" should remain
	expected := map[string]bool{"quick": true, "brown": true, "fox": true, "jumps": true, "lazy": true, "dog": true}
	for _, term := range got {
		if !expected[term] {
			t.Errorf("Unexpected term %q in result", term)
		}
	}
}

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
	for i := 0; i < 20; i++ {
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
