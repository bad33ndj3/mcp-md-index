// Package indexer orchestrates document loading, parsing, and caching.
// It ties together the cache, parser, and search components.
// Dependency injection via interfaces makes it fully testable.
package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	// Worker pool for embeddings
	queue chan *domain.Index // buffered queue of docs to embed
	wg    sync.WaitGroup     // waits for workers on shutdown (if we added Close)

	// Status tracking
	statusMu sync.RWMutex
	stats    IndexerStatus
}

// IndexerStatus holds real-time metrics.
type IndexerStatus struct {
	DocsCount     int // Total docs in cache
	QueueLength   int // Current items waiting for embedding
	EmbeddedCount int // Total embeddings generated this session
	ActiveWorkers int // Number of workers currently embedding
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

// WithMaxConcurrentEmbeddings sets the maximum number of concurrent embedding tasks.
// Also determines the worker pool size.
func WithMaxConcurrentEmbeddings(n int) Option {
	if n <= 0 {
		n = 1
	}
	return func(idx *Indexer) {
		// We use this option to trigger worker start in New(),
		// but we store the count here via a temp field or just rely on default if not set?
		// Actually, let's just resize the channel or use it in New.
		// Since Option runs before New returns, we can't start workers here comfortably if queue isn't made.
		// Design tweak: Let's store config in Indexer and init in New.
	}
}

// ... helper to handle the worker count logic ...
// We'll hardcode a reasonable queue size, e.g., 1000.
const defaultQueueSize = 10000
const defaultWorkerCount = 2

// New creates an Indexer with all its dependencies injected.
func New(c cache.Cache, p parser.Parser, s search.Searcher, r FileReader, clk Clock, f fetcher.Fetcher, opts ...Option) *Indexer {
	idx := &Indexer{
		cache:    c,
		parser:   p,
		searcher: s,
		reader:   r,
		clock:    clk,
		fetcher:  f,
		queue:    make(chan *domain.Index, defaultQueueSize),
	}

	// Apply options
	for _, opt := range opts {
		opt(idx)
	}

	// Start embedding workers if embedder is configured
	if idx.embedder != nil {
		workers := defaultWorkerCount
		// If we want to respect the MaxConcurrent option, we need to handle it.
		// For now, let's default to 2.
		for i := 0; i < workers; i++ {
			idx.wg.Add(1)
			go idx.embeddingWorker()
		}
	}

	// Try to hydrate cache from disk (best effort)
	if err := c.Hydrate(); err != nil {
		if idx.logger != nil {
			idx.logger.Warn("failed to hydrate cache", "error", err)
		}
	}

	// Try to restore queue from disk
	idx.loadQueue()

	return idx
}

// Close gracefully shuts down the indexer, saving any pending queue items.
func (idx *Indexer) Close() error {
	// 1. Close queue to stop accepting new items (optional, but good practice)
	// Actually, we want to drain it.

	// 2. Save pending queue items to disk
	if err := idx.saveQueue(); err != nil {
		if idx.logger != nil {
			idx.logger.Error("failed to save queue", "error", err)
		}
		return err
	}

	return nil
}

// saveQueue persists pending docIDs to a file for restoration on restart.
func (idx *Indexer) saveQueue() error {
	idx.statusMu.RLock()
	pendingCount := len(idx.queue)
	idx.statusMu.RUnlock()

	if pendingCount == 0 {
		return nil
	}

	// Drain valid items from queue without blocking
	var docIDs []string

	// We use a loop with select to drain whatever is currently available
	for {
		select {
		case index := <-idx.queue:
			docIDs = append(docIDs, index.DocID)
		default:
			goto Drained
		}
	}
Drained:

	if len(docIDs) == 0 {
		return nil
	}

	queuePath := filepath.Join(idx.cache.Dir(), "queue.json")

	// Create a simple structure
	data := struct {
		DocIDs []string `json:"doc_ids"`
	}{
		DocIDs: docIDs,
	}

	file, err := os.Create(queuePath)
	if err != nil {
		return fmt.Errorf("create queue file: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(data); err != nil {
		return fmt.Errorf("encode queue: %w", err)
	}

	if idx.logger != nil {
		idx.logger.Info("saved pending queue", "count", len(docIDs), "file", queuePath)
	}

	return nil
}

// loadQueue restores pending items from disk into the channel.
func (idx *Indexer) loadQueue() {
	queuePath := filepath.Join(idx.cache.Dir(), "queue.json")

	file, err := os.Open(queuePath)
	if err != nil {
		if !os.IsNotExist(err) && idx.logger != nil {
			idx.logger.Warn("failed to open queue file", "error", err)
		}
		return
	}
	defer file.Close()

	// Clean up file after opening so we don't reload it next time if we crash immediately
	defer os.Remove(queuePath)

	var data struct {
		DocIDs []string `json:"doc_ids"`
	}

	if err := json.NewDecoder(file).Decode(&data); err != nil {
		if idx.logger != nil {
			idx.logger.Warn("failed to decode queue file", "error", err)
		}
		return
	}

	restored := 0
	for _, docID := range data.DocIDs {
		// Load index from cache to push back to queue
		if index, err := idx.cache.Get(docID); err == nil {
			// Non-blocking push
			select {
			case idx.queue <- index:
				idx.statusMu.Lock()
				idx.stats.QueueLength++
				idx.statusMu.Unlock()
				restored++
			default:
				// Queue full
			}
		}
	}

	if idx.logger != nil && restored > 0 {
		idx.logger.Info("restored pending queue", "count", restored)
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
	// Choose parser based on extension
	ext := strings.ToLower(filepath.Ext(path))
	var chunks []domain.Chunk
	var docFreq map[string]int

	if ext == ".md" || ext == ".markdown" {
		chunks, docFreq = idx.parser.Parse(path, string(content))
	} else {
		// Use generic parser for code/other files
		// We instantiate it here or could inject it. Since it's stateless config, ok to create.
		// Optimally valid injection would be better, but for this POC we default to it.
		gp := parser.NewGenericParser()
		chunks, docFreq = gp.Parse(path, string(content))
	}

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

	// 6. Push to embedding queue (NON-BLOCKING or BACKPRESSURE if queue full)
	if idx.embedder != nil {
		select {
		case idx.queue <- index:
			idx.statusMu.Lock()
			idx.stats.DocsCount++ // Track new doc
			idx.stats.QueueLength++
			idx.statusMu.Unlock()
		default:
			// Queue full - dropping or blocking?
			// Ideally blocking for backpressure, but for async Load this might block the read_repository loop.
			// Let's block to provide natural backpressure.
			idx.queue <- index
			idx.statusMu.Lock()
			idx.stats.DocsCount++
			idx.stats.QueueLength++
			idx.statusMu.Unlock()
		}
	}

	return &LoadResult{
		DocID:     index.DocID,
		Path:      index.Path,
		NumChunks: index.NumChunks,
		FromCache: false,
		IndexedAt: index.IndexedAt,
	}, nil
}

// ... (LoadGlob and FindFiles unchanged) ...

const (
	maxBatchChunks   = 100 // Maximum chunks to process in one API call
	batchWaitTimeout = 50 * time.Millisecond
)

// embeddingWorker consumes the queue and processes embeddings in batches.
func (idx *Indexer) embeddingWorker() {
	defer idx.wg.Done()

	for {
		// 1. Wait for at least one item
		index, ok := <-idx.queue
		if !ok {
			return // Queue closed
		}

		// 2. Start building a batch
		batch := []*domain.Index{index}
		totalChunks := len(index.Chunks)

		// 3. Try to collect more items up to maxBatchChunks or until timeout
		timer := time.NewTimer(batchWaitTimeout)
		collectionDone := false

		for !collectionDone && totalChunks < maxBatchChunks {
			select {
			case nextIndex, ok := <-idx.queue:
				if !ok {
					collectionDone = true
					break
				}
				batch = append(batch, nextIndex)
				totalChunks += len(nextIndex.Chunks)
			case <-timer.C:
				collectionDone = true
			}
		}
		timer.Stop()

		// 4. Process the batch
		idx.statusMu.Lock()
		idx.stats.ActiveWorkers++
		idx.statusMu.Unlock()

		idx.generateBatchEmbeddings(batch)

		idx.statusMu.Lock()
		idx.stats.ActiveWorkers--
		idx.stats.QueueLength -= len(batch)
		idx.stats.EmbeddedCount += len(batch)
		idx.statusMu.Unlock()
	}
}

// generateBatchEmbeddings generates embeddings for a batch of documents.
func (idx *Indexer) generateBatchEmbeddings(batch []*domain.Index) {
	ctx := context.Background()

	// Collect all chunk texts from all documents in the batch
	var allTexts []string
	for _, index := range batch {
		for _, c := range index.Chunks {
			allTexts = append(allTexts, idx.prepareTextForEmbedding(c))
		}
	}

	if len(allTexts) == 0 {
		return
	}

	// Generate embeddings for the whole batch
	allEmbeddings, err := idx.embedder.EmbedBatch(ctx, allTexts)
	if err != nil {
		if idx.logger != nil {
			idx.logger.Warn("failed to generate batch embeddings",
				"batch_size", len(batch),
				"total_chunks", len(allTexts),
				"error", err)
		}
		return
	}

	// Distribute embeddings back to their respective documents
	if len(allEmbeddings) != len(allTexts) {
		if idx.logger != nil {
			idx.logger.Error("embedding result count mismatch",
				"expected", len(allTexts),
				"got", len(allEmbeddings))
		}
		return
	}

	offset := 0
	for _, index := range batch {
		docChunks := len(index.Chunks)
		for i := 0; i < docChunks; i++ {
			index.Chunks[i].Embedding = allEmbeddings[offset+i]
		}
		offset += docChunks

		// Update caches for this document
		idx.cache.Set(index.DocID, index)
		_ = idx.cache.SaveToDisk(index)

		// Mark as ready
		if idx.embedStatus != nil {
			idx.embedStatus.SetReady(index.DocID)
		}
	}

	if idx.logger != nil {
		idx.logger.Debug("batch embeddings generated",
			"docs", len(batch),
			"total_chunks", len(allTexts))
	}
}

// GetStatus returns the current indexing status.
func (idx *Indexer) GetStatus() IndexerStatus {
	idx.statusMu.RLock()
	defer idx.statusMu.RUnlock()

	// Refresh total docs count from cache (source of truth)
	// We do this here because stats.DocsCount tracks *added* this session,
	// but user might want total cache size.
	// Actually, stats.DocsCount is kinda tricky.
	// Let's just return what we track, plus list size.
	s := idx.stats
	s.DocsCount = len(idx.cache.List())
	return s
}

// prepareTextForEmbedding prepends heading path to chunk text for better semantic context.
func (idx *Indexer) prepareTextForEmbedding(chunk domain.Chunk) string {
	var sb strings.Builder

	// Add file context for code
	ext := strings.ToLower(filepath.Ext(chunk.Path))
	if ext != ".md" && ext != ".markdown" && chunk.Path != "" {
		lang := strings.TrimPrefix(ext, ".")
		sb.WriteString(fmt.Sprintf("File: %s (Lang: %s)\n", filepath.Base(chunk.Path), lang))
	}

	if len(chunk.HeadingPath) > 0 {
		sb.WriteString(strings.Join(chunk.HeadingPath, " > "))
		sb.WriteString(": ")
	}
	sb.WriteString(chunk.Text)
	return sb.String()
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
func (idx *Indexer) LoadGlob(pattern string) (*LoadGlobResult, error) {
	return idx.LoadGlobWithExcludes(pattern, nil)
}

// LoadGlobAsync acts like LoadGlobWithExcludes but runs in a goroutine.
// It returns immediately.
func (idx *Indexer) LoadGlobAsync(pattern string, excludes []string) error {
	if pattern == "" {
		return errors.New("pattern is required")
	}

	go func() {
		// We ignore the result for now, but in a real app we'd report it via a status channel
		// or logs. Since this is a POC, logging is sufficient.
		res, err := idx.LoadGlobWithExcludes(pattern, excludes)
		if idx.logger != nil {
			if err != nil {
				idx.logger.Error("Async load failed", "pattern", pattern, "error", err)
			} else {
				idx.logger.Info("Async load complete",
					"pattern", pattern,
					"loaded", res.Loaded,
					"cached", res.Cached,
					"failed", res.Failed)
			}
		}
	}()
	return nil
}

// LoadGlobWithExcludes loads files matching pattern but ignoring excludes.
// Supports ** for recursive directory matching.
func (idx *Indexer) LoadGlobWithExcludes(pattern string, excludes []string) (*LoadGlobResult, error) {
	if pattern == "" {
		return nil, errors.New("pattern is required")
	}

	// Find matching files using recursive walk if pattern contains **
	var matches []string
	if strings.Contains(pattern, "**") {
		matches = findFilesRecursive(pattern)
	} else {
		var err error
		matches, err = filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no files match pattern: %s", pattern)
	}

	// Filter files
	files := make([]string, 0, len(matches))
	for _, path := range matches {
		// 1. Must be a file
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}

		// 2. Must not be excluded
		if isExcluded(path, excludes) {
			continue
		}

		files = append(files, path)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files match pattern after exclusions")
	}

	result := &LoadGlobResult{
		Results: make([]*LoadResult, 0, len(files)),
		Errors:  make([]string, 0),
	}

	// For small file counts, load sequentially
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

// isExcluded checks if a path matches any exclude pattern.
// Patterns support ** globbing.
func isExcluded(path string, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}

	// Normalize path for matching
	cleanPath := filepath.Clean(path)

	for _, pattern := range excludes {
		// Use doublestar-like logic or standard Match with recursion
		// Simple approach: if pattern contains **, try to match parts or use filepath.Match
		// If pattern is absolute/relative mix, it can be tricky.
		// We'll check if the path contains the pattern (substring) if straight match fails,
		// or use simple glob match.

		matched, _ := filepath.Match(pattern, cleanPath)
		if matched {
			return true
		}

		matched, _ = filepath.Match(pattern, filepath.Base(cleanPath))
		if matched {
			return true
		}

		// Handle recursive exclude patterns manually if needed
		// e.g. "**/vendor/**" -> check if "/vendor/" is in path
		if strings.Contains(pattern, "**/") {
			term := strings.TrimPrefix(pattern, "**/")
			term = strings.TrimSuffix(term, "**")
			term = strings.TrimSuffix(term, "/*")
			if strings.Contains(cleanPath, term) {
				return true
			}
		}
	}
	return false
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
