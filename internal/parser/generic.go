package parser

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/text"
)

// GenericParser splits text files into chunks based on line count.
// Suitable for source code or unstructured text where headings are absent.
type GenericParser struct {
	ChunkSize int // Lines per chunk (default 60)
	Overlap   int // Lines of overlap (default 10)
}

// NewGenericParser creates a parser with sensible defaults for code.
func NewGenericParser() *GenericParser {
	return &GenericParser{
		ChunkSize: 60,
		Overlap:   10,
	}
}

// Parse splits content into chunks using a sliding window of lines.
func (p *GenericParser) Parse(path, content string) ([]domain.Chunk, map[string]int) {
	lines := strings.Split(content, "\n")
	docID := DocIDForPath(path)
	filename := filepath.Base(path)

	chunkSize := p.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 60
	}
	overlap := p.Overlap
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 2
	}

	var chunks []domain.Chunk
	step := chunkSize - overlap

	for i := 0; i < len(lines); i += step {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		// Ensure we don't produce tiny tail chunks unless it's the only chunk
		if end-i < 10 && len(chunks) > 0 {
			break
		}

		chunkLines := lines[i:end]
		txt := strings.Join(chunkLines, "\n")

		chunk := domain.Chunk{
			ChunkID:     fmt.Sprintf("%s:%d-%d", docID, i+1, end),
			DocID:       docID,
			Path:        path,
			Title:       fmt.Sprintf("Source Code: %s", filename),
			HeadingPath: nil, // Code has no heading structure
			StartLine:   i + 1,
			EndLine:     end,
			Text:        txt,
			Terms:       text.NormalizeTerms(txt),
			HasCode:     true, // Assume generic text is code-like
		}
		chunks = append(chunks, chunk)

		// If we reached the end, stop
		if end == len(lines) {
			break
		}
	}

	// Calculate doc freq
	docFreq := make(map[string]int)
	for _, c := range chunks {
		seen := make(map[string]struct{})
		for _, term := range c.Terms {
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			docFreq[term]++
		}
	}

	return chunks, docFreq
}
