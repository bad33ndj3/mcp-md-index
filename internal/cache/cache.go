// Package cache provides storage for document indexes.
// It supports both in-memory caching (fast, but lost on restart)
// and disk persistence (survives restarts).
//
// The Cache interface allows us to swap implementations for testing.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
)

// ErrNotFound is returned when a requested index doesn't exist.
var ErrNotFound = errors.New("index not found")

// ErrVersionMismatch is returned when the cache version doesn't match.
var ErrVersionMismatch = errors.New("cache version mismatch (delete .mcp-mdx-cache and reindex)")

// Cache defines how indexes are stored and retrieved.
// Having this as an interface lets us create mock implementations for testing.
type Cache interface {
	// Get retrieves an index from memory (fast path).
	// Returns ErrNotFound if not in memory.
	Get(docID string) (*domain.Index, error)

	// Set stores an index in memory.
	Set(docID string, idx *domain.Index)

	// LoadFromDisk retrieves an index from disk cache.
	// Returns ErrNotFound if no cache file exists.
	// Returns ErrVersionMismatch if cache is from old version.
	LoadFromDisk(docID string) (*domain.Index, error)

	// SaveToDisk persists an index to disk for future sessions.
	SaveToDisk(idx *domain.Index) error
}

// FileCache implements Cache using JSON files on disk.
// It maintains an in-memory map for fast repeated access within a session.
type FileCache struct {
	cacheDir string                   // Directory where .index.json files are stored
	mem      map[string]*domain.Index // In-memory cache for current session
	mu       sync.RWMutex             // Protects concurrent access to mem
}

// NewFileCache creates a new FileCache that stores files in the given directory.
// The directory is created if it doesn't exist.
func NewFileCache(cacheDir string) (*FileCache, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	return &FileCache{
		cacheDir: cacheDir,
		mem:      make(map[string]*domain.Index),
	}, nil
}

// Get retrieves an index from the in-memory cache.
func (c *FileCache) Get(docID string) (*domain.Index, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	idx, ok := c.mem[docID]
	if !ok {
		return nil, ErrNotFound
	}
	return idx, nil
}

// Set stores an index in the in-memory cache.
func (c *FileCache) Set(docID string, idx *domain.Index) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mem[docID] = idx
}

// indexPath returns the file path for a given docID's cache file.
func (c *FileCache) indexPath(docID string) string {
	return filepath.Join(c.cacheDir, fmt.Sprintf("%s.index.json", docID))
}

// LoadFromDisk loads an index from the cache directory.
func (c *FileCache) LoadFromDisk(docID string) (*domain.Index, error) {
	path := c.indexPath(docID)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read cache file: %w", err)
	}

	var idx domain.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse cache file: %w", err)
	}

	// Reject caches from incompatible versions
	if idx.Version != domain.CacheVersion {
		return nil, ErrVersionMismatch
	}

	return &idx, nil
}

// SaveToDisk saves an index to the cache directory as a JSON file.
func (c *FileCache) SaveToDisk(idx *domain.Index) error {
	path := c.indexPath(idx.DocID)

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	return nil
}
