// Package indexer orchestrates document loading, parsing, and caching.
// It ties together the cache, parser, and search components.
// Dependency injection via interfaces makes it fully testable.
package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/cache"
	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/embedding"
	"github.com/bad33ndj3/mcp-md-index/internal/fetcher"
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
	fetcher  fetcher.Fetcher

	// Embedding support (optional, experimental)
	embedder    embedding.Embedder // nil if embeddings disabled
	embedStatus *embedding.Status  // tracks per-doc embedding readiness
	logger      *slog.Logger       // for async error logging
}

// Option configures the Indexer.
type Option func(*Indexer)

// WithEmbedder enables async embedding generation during indexing.
func WithEmbedder(e embedding.Embedder, status *embedding.Status) Option {
	return func(idx *Indexer) {
		idx.embedder = e
		idx.embedStatus = status
	}
}

// WithLogger sets a logger for async operations.
func WithLogger(l *slog.Logger) Option {
	return func(idx *Indexer) {
		idx.logger = l
	}
}

// New creates an Indexer with all its dependencies injected.
// This is the constructor pattern for dependency injection.
func New(c cache.Cache, p parser.Parser, s search.Searcher, r FileReader, clk Clock, f fetcher.Fetcher, opts ...Option) *Indexer {
	idx := &Indexer{
		cache:    c,
		parser:   p,
		searcher: s,
		reader:   r,
		clock:    clk,
		fetcher:  f,
	}
	for _, opt := range opts {
		opt(idx)
	}
	return idx
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

	// 6. Generate embeddings in background (NON-BLOCKING)
	if idx.embedder != nil {
		go idx.generateEmbeddingsAsync(index)
	}

	return &LoadResult{
		DocID:     index.DocID,
		Path:      index.Path,
		NumChunks: index.NumChunks,
		FromCache: false,
		IndexedAt: index.IndexedAt,
	}, nil
}

// LoadGlobResult contains summary of bulk loading operation.
type LoadGlobResult struct {
	Loaded  int      // Number of files successfully loaded
	Cached  int      // How many were already cached
	Failed  int      // How many failed to load
	Errors  []string // Error messages for failed files
	Results []*LoadResult
}

// loadJobResult holds the result of loading a single file.
type loadJobResult struct {
	path   string
	result *LoadResult
	err    error
}

// LoadGlob loads all files matching a glob pattern.
// Supports ** for recursive directory matching (e.g., "docs/**/*.md").
// Uses parallel workers for improved performance on multi-core systems.
func (idx *Indexer) LoadGlob(pattern string) (*LoadGlobResult, error) {
	if pattern == "" {
		return nil, errors.New("pattern is required")
	}

	// Find matching files using recursive walk if pattern contains **
	var matches []string
	if strings.Contains(pattern, "**") {
		// Handle recursive glob with **
		matches = findFilesRecursive(pattern)
	} else {
		// Use standard glob for simple patterns
		var err error
		matches, err = filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no files match pattern: %s", pattern)
	}

	// Filter out directories upfront
	files := make([]string, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, path)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files match pattern: %s", pattern)
	}

	result := &LoadGlobResult{
		Results: make([]*LoadResult, 0, len(files)),
		Errors:  make([]string, 0),
	}

	// For small file counts, load sequentially (avoid goroutine overhead)
	if len(files) <= 2 {
		for _, path := range files {
			loadResult, err := idx.Load(path)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			result.Loaded++
			if loadResult.FromCache {
				result.Cached++
			}
			result.Results = append(result.Results, loadResult)
		}
		return result, nil
	}

	// Use worker pool for parallel loading
	const maxWorkers = 4
	jobs := make(chan string, len(files))
	results := make(chan loadJobResult, len(files))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				loadResult, err := idx.Load(path)
				results <- loadJobResult{path: path, result: loadResult, err: err}
			}
		}()
	}

	// Send jobs
	go func() {
		for _, path := range files {
			jobs <- path
		}
		close(jobs)
	}()

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for r := range results {
		if r.err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", r.path, r.err))
			continue
		}
		result.Loaded++
		if r.result.FromCache {
			result.Cached++
		}
		result.Results = append(result.Results, r.result)
	}

	return result, nil
}

// findFilesRecursive finds files matching a pattern with ** support.
// Example: "docs/**/*.md" matches all .md files in docs/ recursively.
func findFilesRecursive(pattern string) []string {
	var matches []string

	// Split pattern into base dir and file pattern
	// e.g., "docs/**/*.md" -> base="docs", filePattern="*.md"
	parts := strings.Split(pattern, "**")
	baseDir := strings.TrimSuffix(parts[0], "/")
	if baseDir == "" {
		baseDir = "."
	}

	filePattern := "*"
	if len(parts) > 1 {
		filePattern = strings.TrimPrefix(parts[1], "/")
		if filePattern == "" {
			filePattern = "*"
		}
	}

	filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		// Match the filename against the pattern
		matched, _ := filepath.Match(filePattern, filepath.Base(path))
		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	return matches
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

// QueryAll searches all cached documents and returns combined results.
// Results are merged by BM25 score across all documents.
func (idx *Indexer) QueryAll(prompt string, maxTokens int) (string, error) {
	if prompt == "" {
		return "", errors.New("prompt is required")
	}

	docIDs := idx.cache.List()
	if len(docIDs) == 0 {
		return "", errors.New("no documents loaded (use docs_load or site_load first)")
	}

	// Collect results from all documents
	var results []string
	tokensUsed := 0

	for _, docID := range docIDs {
		index, err := idx.cache.Get(docID)
		if err != nil {
			continue // Skip if not in memory
		}

		// Get per-document results with remaining token budget
		remaining := maxTokens - tokensUsed
		if remaining <= 0 {
			break
		}

		excerpt := idx.searcher.Search(index, prompt, remaining)
		if excerpt != "" && !strings.Contains(excerpt, "No relevant excerpts") {
			results = append(results, excerpt)
			// Rough token estimate: ~4 chars per token
			tokensUsed += len(excerpt) / 4
		}
	}

	if len(results) == 0 {
		return "No relevant excerpts found in any loaded document.", nil
	}

	return strings.Join(results, "\n\n---\n\n"), nil
}

// SiteLoadResult contains information about a loaded site.
type SiteLoadResult struct {
	DocID     string
	URL       string
	NumChunks int
	FromCache bool
	IndexedAt time.Time
}

// docIDForURL generates a unique document ID from a URL.
func docIDForURL(urlStr string) string {
	h := sha256.Sum256([]byte(urlStr))
	return hex.EncodeToString(h[:8]) // 16-char hex prefix
}

// LoadSite fetches a URL, converts HTML to markdown, and caches it.
// If already cached and force is false, returns the cached version.
func (idx *Indexer) LoadSite(urlStr string, force bool) (*SiteLoadResult, error) {
	if urlStr == "" {
		return nil, errors.New("url is required")
	}

	if idx.fetcher == nil {
		return nil, errors.New("site loading not configured (no fetcher)")
	}

	docID := docIDForURL(urlStr)

	// Skip cache if force refresh requested
	if !force {
		// 1. Check in-memory cache first (fastest path)
		if cached, err := idx.cache.Get(docID); err == nil {
			return &SiteLoadResult{
				DocID:     cached.DocID,
				URL:       cached.Path, // We store URL in Path field
				NumChunks: cached.NumChunks,
				FromCache: true,
				IndexedAt: cached.IndexedAt,
			}, nil
		}

		// 2. Try disk cache (survives restarts)
		if cached, err := idx.cache.LoadFromDisk(docID); err == nil {
			// Validate: same URL
			if cached.Path == urlStr {
				idx.cache.Set(docID, cached)
				return &SiteLoadResult{
					DocID:     cached.DocID,
					URL:       cached.Path,
					NumChunks: cached.NumChunks,
					FromCache: true,
					IndexedAt: cached.IndexedAt,
				}, nil
			}
			// URL changed (hash collision unlikely, but possible)
		}
	}

	// 3. Fetch and convert to markdown
	markdown, err := idx.fetcher.FetchAsMarkdown(urlStr)
	if err != nil {
		return nil, fmt.Errorf("fetch site: %w", err)
	}

	// 4. Save markdown to a local file for source links
	localPath, err := idx.cache.SaveMarkdown(docID, markdown)
	if err != nil {
		return nil, fmt.Errorf("save markdown: %w", err)
	}

	// 5. Hash the content for change detection
	contentHash := sha256.Sum256([]byte(markdown))
	fileHash := hex.EncodeToString(contentHash[:])

	// 6. Parse and index using the LOCAL path (so source links work)
	chunks, docFreq := idx.parser.Parse(localPath, markdown)
	index := &domain.Index{
		DocID:     docID,
		Path:      localPath, // Use local path so source links are openable
		SourceURL: urlStr,    // Store original URL for display
		FileHash:  fileHash,
		IndexedAt: idx.clock.Now(),
		Chunks:    chunks,
		DocFreq:   docFreq,
		NumChunks: len(chunks),
		Version:   domain.CacheVersion,
	}

	// 7. Save to both memory and disk
	idx.cache.Set(docID, index)
	if err := idx.cache.SaveToDisk(index); err != nil {
		return nil, fmt.Errorf("save cache: %w", err)
	}

	return &SiteLoadResult{
		DocID:     index.DocID,
		URL:       urlStr,
		NumChunks: index.NumChunks,
		FromCache: false,
		IndexedAt: index.IndexedAt,
	}, nil
}

// DocInfo contains summary information about a cached document.
type DocInfo struct {
	DocID     string
	Path      string
	SourceURL string // Original URL for site_load entries (empty for local files)
	NumChunks int
	IndexedAt time.Time
}

// List returns information about all documents currently in memory cache.
func (idx *Indexer) List() []DocInfo {
	docIDs := idx.cache.List()
	docs := make([]DocInfo, 0, len(docIDs))

	for _, docID := range docIDs {
		if index, err := idx.cache.Get(docID); err == nil {
			docs = append(docs, DocInfo{
				DocID:     index.DocID,
				Path:      index.Path,
				SourceURL: index.SourceURL,
				NumChunks: index.NumChunks,
				IndexedAt: index.IndexedAt,
			})
		}
	}
	return docs
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

// generateEmbeddingsAsync generates embeddings in background and updates cache.
// This runs as a goroutine to keep Load() non-blocking.
func (idx *Indexer) generateEmbeddingsAsync(index *domain.Index) {
	ctx := context.Background()

	// Collect chunk texts
	texts := make([]string, len(index.Chunks))
	for i, c := range index.Chunks {
		texts[i] = c.Text
	}

	// Generate embeddings (this is the slow part, ~100-500ms)
	embeddings, err := idx.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		// Log error but don't fail - BM25 still works
		if idx.logger != nil {
			idx.logger.Warn("failed to generate embeddings",
				"doc_id", index.DocID,
				"error", err)
		}
		return
	}

	// Update chunks with embeddings
	if len(embeddings) == len(index.Chunks) {
		for i := range index.Chunks {
			index.Chunks[i].Embedding = embeddings[i]
		}

		// Update caches
		idx.cache.Set(index.DocID, index)
		_ = idx.cache.SaveToDisk(index) // Best-effort, already saved without embeddings

		// Mark as ready for hybrid search
		if idx.embedStatus != nil {
			idx.embedStatus.SetReady(index.DocID)
		}

		if idx.logger != nil {
			idx.logger.Debug("embeddings generated",
				"doc_id", index.DocID,
				"chunks", len(index.Chunks))
		}
	}
}
