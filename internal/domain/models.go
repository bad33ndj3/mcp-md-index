// Package domain contains core data types used across the mcp-mdx server.
// These are pure data structures with no behavior - making them easy to understand
// and test. Think of them as the "nouns" of our application.
package domain

import "time"

// CacheVersion is incremented when the cache format changes.
// This ensures old, incompatible caches are rejected and rebuilt.
const CacheVersion = 4

// DefaultMaxTokens is the default token limit for query responses.
const DefaultMaxTokens = 500

// CodeBlock represents a fenced code block extracted from markdown.
type CodeBlock struct {
	Language string `json:"language,omitempty"` // e.g., "go", "yaml", "bash"
	Code     string `json:"code"`
	Line     int    `json:"line"` // Starting line number
}

// TableRow represents a row from a markdown table.
// Used to index API docs with field/type/description tables.
type TableRow struct {
	Cells []string `json:"cells"` // Cell contents
	Line  int      `json:"line"`  // Line number
}

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

	// HeadingPath is the breadcrumb of parent headings for context
	// e.g., ["NATS Guide", "Consumers", "Durable Consumers"]
	HeadingPath []string `json:"heading_path,omitempty"`

	// StartLine is the 1-indexed line where this chunk begins
	StartLine int `json:"start_line"`

	// EndLine is the 1-indexed line where this chunk ends
	EndLine int `json:"end_line"`

	// Text is the raw content of the chunk (including the heading)
	Text string `json:"text"`

	// Terms is a list of normalized, searchable words extracted from Text.
	// Stopwords like "the", "and", "or" are removed; everything is lowercased.
	Terms []string `json:"terms"`

	// CodeBlocks are fenced code blocks extracted from this chunk
	CodeBlocks []CodeBlock `json:"code_blocks,omitempty"`

	// TableRows are markdown table rows extracted from this chunk
	TableRows []TableRow `json:"table_rows,omitempty"`

	// HasCode indicates if this chunk contains code blocks (for quick filtering)
	HasCode bool `json:"has_code,omitempty"`
}

// Index represents a fully parsed and indexed markdown document.
// Once created, an Index is cached to disk so we don't re-parse on every query.
type Index struct {
	// DocID is a unique identifier derived from the file path (SHA256 prefix)
	DocID string `json:"doc_id"`

	// Path is the local file path that was indexed (may be cache path for URLs)
	Path string `json:"path"`

	// SourceURL is the original URL for site_load entries (empty for local files)
	SourceURL string `json:"source_url,omitempty"`

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
