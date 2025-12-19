package embedding

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ollama/ollama/api"
)

// OllamaEmbedder wraps the Ollama API for embedding generation.
type OllamaEmbedder struct {
	client *api.Client
	model  string
}

// NewOllamaEmbedder creates an embedder connected to Ollama.
func NewOllamaEmbedder(cfg Config) (*OllamaEmbedder, error) {
	u, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("parse ollama host: %w", err)
	}

	client := api.NewClient(u, http.DefaultClient)
	return &OllamaEmbedder{
		client: client,
		model:  cfg.Model,
	}, nil
}

// Embed generates a single embedding vector.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.Embed(ctx, &api.EmbedRequest{
		Model: e.model,
		Input: text,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}

	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings")
	}

	return resp.Embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// This is more efficient than calling Embed repeatedly.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Ollama's Embed API can process multiple inputs
	results := make([][]float32, len(texts))

	// Process in batches to avoid overwhelming the API
	const batchSize = 10
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		for j, text := range batch {
			resp, err := e.client.Embed(ctx, &api.EmbedRequest{
				Model: e.model,
				Input: text,
			})
			if err != nil {
				return nil, fmt.Errorf("ollama embed batch[%d]: %w", i+j, err)
			}

			if len(resp.Embeddings) == 0 {
				return nil, fmt.Errorf("ollama returned no embeddings for batch[%d]", i+j)
			}

			results[i+j] = resp.Embeddings[0]
		}
	}

	return results, nil
}

// Available checks if Ollama is reachable and the model is available.
func (e *OllamaEmbedder) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Try to get version to check connectivity
	_, err := e.client.Version(ctx)
	return err == nil
}
