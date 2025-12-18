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

// tokenRe matches alphanumeric words (used for term extraction).
var tokenRe = regexp.MustCompile(`[a-zA-Z0-9_]+`)

// stopwords are common words we filter out during term extraction.
// They don't help distinguish between chunks, so we skip them.
var stopwords = map[string]struct{}{
	"the": {}, "and": {}, "or": {}, "to": {}, "of": {}, "in": {},
	"a": {}, "an": {}, "for": {}, "with": {}, "on": {}, "is": {},
	"are": {}, "as": {}, "by": {}, "be": {}, "over": {},
}

// NormalizeTerms converts text into a list of searchable terms.
// It lowercases, tokenizes, removes stopwords, and skips single-char tokens.
//
// Example: "The Consumer is configured" → ["consumer", "configured"]
func NormalizeTerms(text string) []string {
	text = strings.ToLower(text)
	raw := tokenRe.FindAllString(text, -1)

	out := make([]string, 0, len(raw))
	for _, t := range raw {
		// Skip single-character tokens (usually not meaningful)
		if len(t) <= 1 {
			continue
		}
		// Skip stopwords
		if _, isStopword := stopwords[t]; isStopword {
			continue
		}
		out = append(out, t)
	}
	return out
}

// DocIDForPath generates a unique, stable identifier for a file path.
// Uses SHA256 of the absolute path, truncated to 16 chars.
func DocIDForPath(path string) string {
	abs, _ := filepath.Abs(path)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
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

	var chunks []domain.Chunk

	// flush saves the current buffer as a chunk
	flush := func(endLine int) {
		text := strings.TrimSpace(strings.Join(curBuf, "\n"))
		if text == "" {
			curBuf = curBuf[:0]
			curStart = endLine + 1
			return
		}
		chunks = append(chunks, domain.Chunk{
			ChunkID:   fmt.Sprintf("%s:%d-%d", docID, curStart, endLine),
			DocID:     docID,
			Path:      path,
			Title:     curTitle,
			StartLine: curStart,
			EndLine:   endLine,
			Text:      text,
			Terms:     NormalizeTerms(text),
		})
		curBuf = curBuf[:0]
		curStart = endLine + 1
	}

	// Process each line
	for i, line := range lines {
		ln := i + 1 // 1-indexed line number

		// Check if this is a heading
		if m := headingRe.FindStringSubmatch(line); m != nil {
			// If we have enough content, flush before starting new section
			if len(curBuf) >= minLines {
				flush(ln - 1)
			}
			curTitle = m[2] // Use heading text as new title
			curBuf = append(curBuf, line)
			blankRun = 0
			continue
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
