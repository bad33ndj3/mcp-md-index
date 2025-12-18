// Package parser handles splitting markdown documents into searchable chunks.
// It's designed to be simple and predictable - no complex NLP, just practical rules.
package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/text"
)

// Parser defines how markdown content is split into chunks.
// Having this as an interface allows different chunking strategies if needed.
type Parser interface {
	// Parse splits markdown content into chunks and returns:
	// - chunks: the list of searchable sections
	// - docFreq: how many chunks contain each term (for BM25 scoring)
	Parse(path, content string) (chunks []domain.Chunk, docFreq map[string]int)
}

// MarkdownParser splits markdown files by headings and paragraph breaks.
// It's the default implementation used in production.
type MarkdownParser struct {
	// MaxLinesPerChunk is the hard limit before forcing a new chunk (default: 120)
	MaxLinesPerChunk int

	// MinLinesPerChunk is the minimum before a heading triggers a new chunk (default: 12)
	MinLinesPerChunk int
}

// NewMarkdownParser creates a parser with sensible defaults.
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{
		MaxLinesPerChunk: 120,
		MinLinesPerChunk: 12,
	}
}

// headingRe matches markdown headings like "## Introduction" or "# Title".
var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

// codeBlockStartRe matches the start of a fenced code block: ```language
var codeBlockStartRe = regexp.MustCompile("^```(\\w*)\\s*$")

// codeBlockEndRe matches the end of a fenced code block: ```
var codeBlockEndRe = regexp.MustCompile("^```\\s*$")

// DocIDForPath generates a unique, stable identifier for a file path.
// Uses SHA256 of the absolute path, truncated to 16 chars.
func DocIDForPath(path string) string {
	abs, _ := filepath.Abs(path)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

// isSeparatorCell checks if a table cell is just separator dashes (e.g., "---" or ":---:")
func isSeparatorCell(cell string) bool {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return true
	}
	// Table separator rows look like: | --- | :---: | ---: |
	for _, r := range cell {
		if r != '-' && r != ':' {
			return false
		}
	}
	return true
}

// headingStack tracks the current heading hierarchy for breadcrumb paths.
type headingStack struct {
	levels []int    // Heading level (1-6)
	titles []string // Heading text
}

func (h *headingStack) push(level int, title string) {
	// Pop any headers at same or lower level
	for len(h.levels) > 0 && h.levels[len(h.levels)-1] >= level {
		h.levels = h.levels[:len(h.levels)-1]
		h.titles = h.titles[:len(h.titles)-1]
	}
	h.levels = append(h.levels, level)
	h.titles = append(h.titles, title)
}

func (h *headingStack) path() []string {
	if len(h.titles) == 0 {
		return nil
	}
	// Return a copy to avoid mutation
	result := make([]string, len(h.titles))
	copy(result, h.titles)
	return result
}

// Parse splits a markdown file into chunks.
// Each chunk corresponds roughly to a heading and its content.
func (p *MarkdownParser) Parse(path, content string) ([]domain.Chunk, map[string]int) {
	lines := strings.Split(content, "\n")
	docID := DocIDForPath(path)

	// Defaults if not set
	maxLines := p.MaxLinesPerChunk
	minLines := p.MinLinesPerChunk
	if maxLines == 0 {
		maxLines = 120
	}
	if minLines == 0 {
		minLines = 12
	}

	// State for building chunks
	curTitle := filepath.Base(path) // Use filename as initial title
	curStart := 1                   // 1-indexed line number
	curBuf := make([]string, 0, 256)
	blankRun := 0 // Count consecutive blank lines

	// Heading hierarchy for breadcrumb paths
	headings := &headingStack{}

	// Code block extraction state
	var codeBlocks []domain.CodeBlock
	inCodeBlock := false
	codeBlockLang := ""
	codeBlockStart := 0
	var codeBlockBuf []string

	// Table extraction state
	var tableRows []domain.TableRow

	var chunks []domain.Chunk

	// flush saves the current buffer as a chunk
	flush := func(endLine int) {
		txt := strings.TrimSpace(strings.Join(curBuf, "\n"))
		if txt == "" {
			curBuf = curBuf[:0]
			curStart = endLine + 1
			codeBlocks = nil
			tableRows = nil
			return
		}

		chunk := domain.Chunk{
			ChunkID:     fmt.Sprintf("%s:%d-%d", docID, curStart, endLine),
			DocID:       docID,
			Path:        path,
			Title:       curTitle,
			HeadingPath: headings.path(),
			StartLine:   curStart,
			EndLine:     endLine,
			Text:        txt,
			Terms:       text.NormalizeTerms(txt), // Use shared package
			CodeBlocks:  codeBlocks,
			TableRows:   tableRows,
			HasCode:     len(codeBlocks) > 0,
		}
		chunks = append(chunks, chunk)

		curBuf = curBuf[:0]
		curStart = endLine + 1
		codeBlocks = nil
		tableRows = nil
	}

	// parseTableRow extracts cells from a markdown table row
	parseTableRow := func(line string) []string {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			return nil
		}
		// Split by | and clean up
		parts := strings.Split(line, "|")
		cells := make([]string, 0, len(parts))
		for _, p := range parts {
			cell := strings.TrimSpace(p)
			if cell != "" && !isSeparatorCell(cell) {
				cells = append(cells, cell)
			}
		}
		return cells
	}

	// Process each line
	for i, line := range lines {
		ln := i + 1 // 1-indexed line number

		// Handle code block boundaries
		if inCodeBlock {
			if codeBlockEndRe.MatchString(line) {
				// End of code block
				codeBlocks = append(codeBlocks, domain.CodeBlock{
					Language: codeBlockLang,
					Code:     strings.Join(codeBlockBuf, "\n"),
					Line:     codeBlockStart,
				})
				inCodeBlock = false
				codeBlockBuf = nil
				curBuf = append(curBuf, line)
				continue
			}
			codeBlockBuf = append(codeBlockBuf, line)
			curBuf = append(curBuf, line)
			continue
		}

		// Check for code block start
		if m := codeBlockStartRe.FindStringSubmatch(line); m != nil {
			inCodeBlock = true
			codeBlockLang = m[1]
			codeBlockStart = ln
			codeBlockBuf = nil
			curBuf = append(curBuf, line)
			continue
		}

		// Check if this is a heading
		if m := headingRe.FindStringSubmatch(line); m != nil {
			level := len(m[1]) // Number of # characters
			title := m[2]

			// If we have enough content, flush before starting new section
			if len(curBuf) >= minLines {
				flush(ln - 1)
			}

			headings.push(level, title)
			curTitle = title
			curBuf = append(curBuf, line)
			blankRun = 0
			continue
		}

		// Check for table rows (starts with |)
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			cells := parseTableRow(line)
			if len(cells) > 0 {
				tableRows = append(tableRows, domain.TableRow{
					Cells: cells,
					Line:  ln,
				})
			}
		}

		// Track blank lines (used to split on paragraph breaks)
		if strings.TrimSpace(line) == "" {
			blankRun++
		} else {
			blankRun = 0
		}

		curBuf = append(curBuf, line)

		// Force split if we hit max lines or 4+ blank lines in a row
		if len(curBuf) >= maxLines || blankRun >= 4 {
			flush(ln)
			blankRun = 0
		}
	}

	// Don't forget the last chunk
	if len(curBuf) > 0 {
		flush(len(lines))
	}

	// Calculate document frequency (how many chunks contain each term)
	// This is used in BM25 scoring - rare terms are more significant
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

// NormalizeTerms is exported for backward compatibility.
// New code should use text.NormalizeTerms directly.
func NormalizeTerms(s string) []string {
	return text.NormalizeTerms(s)
}
