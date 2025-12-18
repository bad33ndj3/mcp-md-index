package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
)

// TestFileCache_SetGet verifies in-memory round-trip storage.
func TestFileCache_SetGet(t *testing.T) {
	cache, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}

	idx := &domain.Index{
		DocID:     "test123",
		Path:      "docs/test.md",
		NumChunks: 5,
		Version:   domain.CacheVersion,
	}

	// Before Set, Get should return ErrNotFound
	_, err = cache.Get("test123")
	if err != ErrNotFound {
		t.Errorf("Get before Set: expected ErrNotFound, got %v", err)
	}

	// After Set, Get should return the index
	cache.Set("test123", idx)
	got, err := cache.Get("test123")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}

	if got.DocID != idx.DocID || got.Path != idx.Path {
		t.Errorf("Get returned wrong index: got %+v, want %+v", got, idx)
	}
}

// TestFileCache_DiskRoundTrip verifies saving to and loading from disk.
func TestFileCache_DiskRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewFileCache(tmpDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}

	idx := &domain.Index{
		DocID:     "persist123",
		Path:      "docs/persist.md",
		FileHash:  "abc123hash",
		IndexedAt: time.Now().Truncate(time.Second), // Truncate for JSON round-trip
		Chunks: []domain.Chunk{
			{ChunkID: "persist123:1-10", Title: "Introduction", Text: "Hello world"},
		},
		DocFreq:   map[string]int{"hello": 1, "world": 1},
		NumChunks: 1,
		Version:   domain.CacheVersion,
	}

	// Save to disk
	if err := cache.SaveToDisk(idx); err != nil {
		t.Fatalf("SaveToDisk: %v", err)
	}

	// Verify file exists
	expectedPath := filepath.Join(tmpDir, "persist123.index.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Cache file not created at %s", expectedPath)
	}

	// Load from disk (simulating a fresh process)
	loaded, err := cache.LoadFromDisk("persist123")
	if err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}

	if loaded.DocID != idx.DocID {
		t.Errorf("DocID mismatch: got %q, want %q", loaded.DocID, idx.DocID)
	}
	if loaded.Path != idx.Path {
		t.Errorf("Path mismatch: got %q, want %q", loaded.Path, idx.Path)
	}
	if loaded.NumChunks != idx.NumChunks {
		t.Errorf("NumChunks mismatch: got %d, want %d", loaded.NumChunks, idx.NumChunks)
	}
	if len(loaded.Chunks) != len(idx.Chunks) {
		t.Errorf("Chunks length mismatch: got %d, want %d", len(loaded.Chunks), len(idx.Chunks))
	}
}

// TestFileCache_LoadNotFound verifies behavior when cache file doesn't exist.
func TestFileCache_LoadNotFound(t *testing.T) {
	cache, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}

	_, err = cache.LoadFromDisk("nonexistent")
	if err != ErrNotFound {
		t.Errorf("LoadFromDisk: expected ErrNotFound, got %v", err)
	}
}

// TestFileCache_VersionMismatch verifies old caches are rejected.
func TestFileCache_VersionMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewFileCache(tmpDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}

	// Save an index with the wrong version
	idx := &domain.Index{
		DocID:   "oldversion",
		Version: domain.CacheVersion + 999, // Simulate an incompatible version
	}
	if err := cache.SaveToDisk(idx); err != nil {
		t.Fatalf("SaveToDisk: %v", err)
	}

	// Loading should fail with version mismatch
	_, err = cache.LoadFromDisk("oldversion")
	if err != ErrVersionMismatch {
		t.Errorf("LoadFromDisk: expected ErrVersionMismatch, got %v", err)
	}
}
