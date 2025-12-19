package search

import (
	"context"
	"testing"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/embedding"
)

type mockEmbedder struct {
	available bool
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{1.0, 0.0}, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	res := make([][]float32, len(texts))
	for i := range texts {
		res[i] = []float32{1.0, 0.0}
	}
	return res, nil
}

func (m *mockEmbedder) Available(ctx context.Context) bool {
	return m.available
}

func TestHybridSearcher(t *testing.T) {
	status := embedding.NewStatus()
	embedder := &mockEmbedder{available: true}

	idx := &domain.Index{
		DocID: "test-doc",
		Chunks: []domain.Chunk{
			{
				ChunkID:   "c1",
				Text:      "apple",
				Terms:     []string{"apple"},
				Embedding: []float32{1.0, 0.0},
			},
			{
				ChunkID:   "c2",
				Text:      "banana",
				Terms:     []string{"banana"},
				Embedding: []float32{0.0, 1.0},
			},
		},
		DocFreq:   map[string]int{"apple": 1, "banana": 1},
		NumChunks: 2,
	}

	t.Run("InitiallyNotReady", func(t *testing.T) {
		searcher := NewHybridSearcher(embedder, status)
		res := searcher.Search(idx, "apple", 100)
		if res == "" || !contains(res, "apple") {
			t.Errorf("expected BM25 result for apple")
		}
	})

	status.SetReady("test-doc")

	t.Run("HybridRRF", func(t *testing.T) {
		searcher := NewHybridSearcher(embedder, status)
		searcher.WithFusionMethod(FusionMethodRRF, 0, 0, 60)
		res := searcher.Search(idx, "apple", 100)
		if res == "" {
			t.Errorf("expected Hybrid (RRF) result")
		}
	})

	t.Run("HybridWeighted", func(t *testing.T) {
		searcher := NewHybridSearcher(embedder, status)
		searcher.WithFusionMethod(FusionMethodWeighted, 0.3, 0.7, 0)
		res := searcher.Search(idx, "apple", 100)
		if res == "" {
			t.Errorf("expected Hybrid (Weighted) result")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr))
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1.0},
		{[]float32{1, 0}, []float32{0, 1}, 0.0},
		{[]float32{1, 0}, []float32{-1, 0}, -1.0},
		{[]float32{1, 1}, []float32{1, 1}, 1.0},
		{[]float32{1, 2, 3}, []float32{1, 2, 3}, 1.0},
	}

	for _, tt := range tests {
		got := cosineSimilarity(tt.a, tt.b)
		if mathAbs(got-tt.want) > 1e-6 {
			t.Errorf("cosineSimilarity(%v, %v) = %f; want %f", tt.a, tt.b, got, tt.want)
		}
	}
}

func mathAbs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
