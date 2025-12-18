// Package indexer orchestrates document loading, parsing, and caching.
// It ties together the cache, parser, and search components.
// Dependency injection via interfaces makes it fully testable.
package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/cache"
	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/parser"
	"github.com/bad33ndj3/mcp-md-index/internal/search"
)

// FileReader abstracts file system access for testability.
// In tests, you can inject a mock that returns controlled content.
type FileReader interface {
	// ReadFile reads the entire contents of a file.
	ReadFile(path string) ([]byte, error)

	// HashFile returns a hash of the file's contents.
	// Used to detect when a file has changed and needs re-indexing.
	HashFile(path string) (string, error)
}

// Clock abstracts time access for reproducible tests.
// In tests, you can inject a mock that returns a fixed time.
type Clock interface {
	Now() time.Time
}

// Indexer orchestrates loading, parsing, and caching documents.
// It's the main entry point for document operations.
type Indexer struct {
	cache    cache.Cache
	parser   parser.Parser
	searcher search.Searcher
	reader   FileReader
	clock    Clock
}

// New creates an Indexer with all its dependencies injected.
// This is the constructor pattern for dependency injection.
func New(c cache.Cache, p parser.Parser, s search.Searcher, r FileReader, clk Clock) *Indexer {
	return &Indexer{
		cache:    c,
		parser:   p,
		searcher: s,
		reader:   r,
		clock:    clk,
	}
}

// LoadResult contains information about a loaded document.
type LoadResult struct {
	DocID     string
	Path      string
	NumChunks int
	FromCache bool
	IndexedAt time.Time
}

// Load indexes a markdown file and caches it.
// If already cached and file hasn't changed, returns cached version.
func (idx *Indexer) Load(path string) (*LoadResult, error) {
	if path == "" {
		return nil, errors.New("path is required")
	}

	docID := parser.DocIDForPath(path)

	// 1. Check in-memory cache first (fastest path)
	if cached, err := idx.cache.Get(docID); err == nil {
		return &LoadResult{
			DocID:     cached.DocID,
			Path:      cached.Path,
			NumChunks: cached.NumChunks,
			FromCache: true,
			IndexedAt: cached.IndexedAt,
		}, nil
	}

	// 2. Read and hash the file
	content, err := idx.reader.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	fileHash, err := idx.reader.HashFile(path)
	if err != nil {
		return nil, fmt.Errorf("hash file: %w", err)
	}

	// 3. Try disk cache (survives restarts)
	if cached, err := idx.cache.LoadFromDisk(docID); err == nil {
		// Validate: same path and file hasn't changed
		if cached.Path == path && cached.FileHash == fileHash {
			idx.cache.Set(docID, cached)
			return &LoadResult{
				DocID:     cached.DocID,
				Path:      cached.Path,
				NumChunks: cached.NumChunks,
				FromCache: true,
				IndexedAt: cached.IndexedAt,
			}, nil
		}
		// File changed, need to re-index
	}

	// 4. Parse and index the document
	chunks, docFreq := idx.parser.Parse(path, string(content))
	index := &domain.Index{
		DocID:     docID,
		Path:      path,
		FileHash:  fileHash,
		IndexedAt: idx.clock.Now(),
		Chunks:    chunks,
		DocFreq:   docFreq,
		NumChunks: len(chunks),
		Version:   domain.CacheVersion,
	}

	// 5. Save to both memory and disk
	idx.cache.Set(docID, index)
	if err := idx.cache.SaveToDisk(index); err != nil {
		return nil, fmt.Errorf("save cache: %w", err)
	}

	return &LoadResult{
		DocID:     index.DocID,
		Path:      index.Path,
		NumChunks: index.NumChunks,
		FromCache: false,
		IndexedAt: index.IndexedAt,
	}, nil
}

// Query searches an indexed document and returns token-bounded excerpts.
func (idx *Indexer) Query(docID, path, prompt string, maxTokens int) (string, error) {
	// Resolve docID from path if not provided
	if docID == "" {
		if path == "" {
			return "", errors.New("doc_id or path is required")
		}
		docID = parser.DocIDForPath(path)
	}

	// 1. Try in-memory cache
	index, err := idx.cache.Get(docID)
	if err != nil {
		// 2. Try disk cache
		index, err = idx.cache.LoadFromDisk(docID)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				return "", errors.New("document not loaded (call docs_load first)")
			}
			return "", fmt.Errorf("load from cache: %w", err)
		}

		// Validate path match if provided
		if path != "" && index.Path != path {
			return "", fmt.Errorf("cache doc_id exists but path differs: cached=%s requested=%s", index.Path, path)
		}

		// Warm up memory cache
		idx.cache.Set(docID, index)
	}

	if prompt == "" {
		return "", errors.New("prompt is required")
	}

	return idx.searcher.Search(index, prompt, maxTokens), nil
}

// OSFileReader is the production implementation using the real filesystem.
type OSFileReader struct{}

// ReadFile reads a file from the real filesystem.
func (OSFileReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// HashFile computes SHA256 of a file's contents.
func (OSFileReader) HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RealClock uses the actual system time.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time {
	return time.Now()
}
