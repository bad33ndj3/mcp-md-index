// Package embedding provides async vector embedding generation via Ollama.
// Embeddings are generated in the background after document loading,
// allowing queries to use BM25 until embeddings are ready.
package embedding

import (
	"context"
	"sync"
)

// Config holds settings for the embedding client.
type Config struct {
	Host  string // Ollama server URL (default: "http://localhost:11434")
	Model string // Embedding model (default: "nomic-embed-text")
}

// DefaultConfig returns sensible defaults for local Ollama.
func DefaultConfig() Config {
	return Config{
		Host:  "http://localhost:11434",
		Model: "nomic-embed-text",
	}
}

// Embedder generates vector embeddings for text.
type Embedder interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts (more efficient).
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Available returns true if the embedding service is reachable.
	Available(ctx context.Context) bool
}

// Status tracks whether embeddings are ready for each document.
// This allows hybrid search to know when to use embeddings vs BM25.
type Status struct {
	mu    sync.RWMutex
	ready map[string]bool // docID -> embeddings ready
}

// NewStatus creates a new embedding status tracker.
func NewStatus() *Status {
	return &Status{ready: make(map[string]bool)}
}

// IsReady checks if embeddings are ready for a document.
func (s *Status) IsReady(docID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready[docID]
}

// SetReady marks embeddings as ready for a document.
func (s *Status) SetReady(docID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready[docID] = true
}

// Clear removes the ready status for a document (e.g., on re-index).
func (s *Status) Clear(docID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ready, docID)
}
