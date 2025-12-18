// Package domain contains core data types used across the mcp-mdx server.
// These are pure data structures with no behavior - making them easy to understand
// and test. Think of them as the "nouns" of our application.
package domain

import "time"

// CacheVersion is incremented when the cache format changes.
// This ensures old, incompatible caches are rejected and rebuilt.
const CacheVersion = 1

// DefaultMaxTokens is the default token limit for query responses.
const DefaultMaxTokens = 500

// Chunk represents a single section of a markdown document.
// Each chunk is a searchable unit with its own title, text, and location.
//
// Example: A heading "## Consumer Configuration" and its content
// becomes one Chunk with Title="Consumer Configuration".
type Chunk struct {
	// ChunkID is a unique identifier like "abc123:42-68" (docID:startLine-endLine)
	ChunkID string `json:"chunk_id"`

	// DocID identifies which document this chunk belongs to
	DocID string `json:"doc_id"`

	// Path is the file path to the source markdown file
	Path string `json:"path"`

	// Title is the heading text (e.g., "Consumer Configuration")
	Title string `json:"title"`

	// StartLine is the 1-indexed line where this chunk begins
	StartLine int `json:"start_line"`

	// EndLine is the 1-indexed line where this chunk ends
	EndLine int `json:"end_line"`

	// Text is the raw content of the chunk (including the heading)
	Text string `json:"text"`

	// Terms is a list of normalized, searchable words extracted from Text.
	// Stopwords like "the", "and", "or" are removed; everything is lowercased.
	Terms []string `json:"terms"`
}

// Index represents a fully parsed and indexed markdown document.
// Once created, an Index is cached to disk so we don't re-parse on every query.
type Index struct {
	// DocID is a unique identifier derived from the file path (SHA256 prefix)
	DocID string `json:"doc_id"`

	// Path is the original file path that was indexed
	Path string `json:"path"`

	// FileHash is a SHA256 hash of the file contents at index time.
	// Used to detect when a file has changed and needs re-indexing.
	FileHash string `json:"file_hash"`

	// IndexedAt is when this index was created
	IndexedAt time.Time `json:"indexed_at"`

	// Chunks is all the searchable sections of the document
	Chunks []Chunk `json:"chunks"`

	// DocFreq maps each term to the number of chunks containing it.
	// Used for BM25 scoring (terms appearing in fewer chunks are more significant).
	DocFreq map[string]int `json:"doc_freq"`

	// NumChunks is len(Chunks), stored for quick access in scoring
	NumChunks int `json:"num_chunks"`

	// Version identifies the cache format version
	Version int `json:"version"`
}
