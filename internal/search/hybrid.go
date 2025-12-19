package search

import (
	"context"
	"math"
	"sort"

	"github.com/bad33ndj3/mcp-md-index/internal/domain"
	"github.com/bad33ndj3/mcp-md-index/internal/embedding"
)

const (
	FusionMethodWeighted = "weighted"
	FusionMethodRRF      = "rrf"
	DefaultRRFK          = 60
)

// HybridSearcher combines BM25 keyword search with embedding cosine similarity.
// Uses BM25 until embeddings are ready, then combines both scores.
type HybridSearcher struct {
	embedder embedding.Embedder
	status   *embedding.Status
	bm25     *BM25Searcher

	// Configuration
	fusionMethod string
	bm25Weight   float64 // for weighted fusion
	embedWeight  float64 // for weighted fusion
	rrfK         int     // for RRF fusion
}

// NewHybridSearcher creates a searcher that combines BM25 and embeddings.
func NewHybridSearcher(e embedding.Embedder, status *embedding.Status) *HybridSearcher {
	return &HybridSearcher{
		embedder:     e,
		status:       status,
		bm25:         NewBM25Searcher(),
		fusionMethod: FusionMethodRRF,
		bm25Weight:   0.3,
		embedWeight:  0.7,
		rrfK:         DefaultRRFK,
	}
}

// WithFusionMethod sets the fusion method and parameters.
func (s *HybridSearcher) WithFusionMethod(method string, bm25Weight, embedWeight float64, rrfK int) *HybridSearcher {
	s.fusionMethod = method
	s.bm25Weight = bm25Weight
	s.embedWeight = embedWeight
	s.rrfK = rrfK
	return s
}

// Search uses hybrid scoring if embeddings ready, else BM25 only.
func (s *HybridSearcher) Search(idx *domain.Index, query string, maxTokens int) string {
	if maxTokens <= 0 {
		maxTokens = domain.DefaultMaxTokens
	}

	// If embeddings not ready for this doc, use BM25 only
	if !s.status.IsReady(idx.DocID) {
		return s.bm25.Search(idx, query, maxTokens)
	}

	// Check if any chunks have embeddings
	hasEmbeddings := false
	for _, c := range idx.Chunks {
		if c.Embedding != nil {
			hasEmbeddings = true
			break
		}
	}
	if !hasEmbeddings {
		return s.bm25.Search(idx, query, maxTokens)
	}

	// Generate query embedding
	ctx := context.Background()
	queryEmbed, err := s.embedder.Embed(ctx, query)
	if err != nil {
		// Fallback to BM25 on error
		return s.bm25.Search(idx, query, maxTokens)
	}

	// Score all chunks with hybrid approach
	scored := s.scoreHybrid(idx, query, queryEmbed)
	if len(scored) == 0 {
		return "No relevant excerpts found in the indexed document."
	}

	return s.bm25.buildResponse(scored, maxTokens)
}

// scoreHybrid selects the configured fusion method.
func (s *HybridSearcher) scoreHybrid(idx *domain.Index, query string, queryEmbed []float32) []scoredChunk {
	if s.fusionMethod == FusionMethodWeighted {
		return s.scoreWeighted(idx, query, queryEmbed)
	}
	return s.scoreRRF(idx, query, queryEmbed)
}

// scoreWeighted combines BM25 and cosine similarity scores using weighted average.
func (s *HybridSearcher) scoreWeighted(idx *domain.Index, query string, queryEmbed []float32) []scoredChunk {
	// Get BM25 scores
	bm25Scores := s.bm25.scoreChunks(idx, query)

	// Build map of BM25 scores and find max for normalization
	maxBM25 := 0.0
	for _, sc := range bm25Scores {
		if sc.score > maxBM25 {
			maxBM25 = sc.score
		}
	}

	bm25Map := make(map[string]float64)
	for _, sc := range bm25Scores {
		normalized := 0.0
		if maxBM25 > 0 {
			normalized = sc.score / maxBM25
		}
		bm25Map[sc.chunk.ChunkID] = normalized
	}

	// Calculate hybrid scores
	results := make([]scoredChunk, 0, len(idx.Chunks))
	for _, chunk := range idx.Chunks {
		if chunk.Embedding == nil {
			// No embedding for this chunk, use BM25 only if it has a score
			if bm25Score, ok := bm25Map[chunk.ChunkID]; ok && bm25Score > 0 {
				results = append(results, scoredChunk{chunk: chunk, score: bm25Score * s.bm25Weight})
			}
			continue
		}

		// Cosine similarity (already normalized to [-1, 1], shift to [0, 1])
		cosineSim := cosineSimilarity(queryEmbed, chunk.Embedding)
		embedScore := (cosineSim + 1) / 2 // Normalize to [0, 1]

		bm25Score := bm25Map[chunk.ChunkID]

		// Weighted combination
		hybridScore := s.bm25Weight*bm25Score + s.embedWeight*embedScore

		if hybridScore > 0 {
			results = append(results, scoredChunk{chunk: chunk, score: hybridScore})
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	return results
}

// scoreRRF combines scores using Reciprocal Rank Fusion.
func (s *HybridSearcher) scoreRRF(idx *domain.Index, query string, queryEmbed []float32) []scoredChunk {
	// 1. Get BM25 ranks
	bm25Scores := s.bm25.scoreChunks(idx, query)
	bm25Ranks := make(map[string]int)
	for i, sc := range bm25Scores {
		bm25Ranks[sc.chunk.ChunkID] = i + 1
	}

	// 2. Get Embedding ranks
	type embedRank struct {
		chunkID string
		score   float64
	}
	embedScores := make([]embedRank, 0, len(idx.Chunks))
	for _, chunk := range idx.Chunks {
		if chunk.Embedding != nil {
			sim := cosineSimilarity(queryEmbed, chunk.Embedding)
			embedScores = append(embedScores, embedRank{chunkID: chunk.ChunkID, score: sim})
		}
	}
	sort.Slice(embedScores, func(i, j int) bool {
		return embedScores[i].score > embedScores[j].score
	})

	embedRanks := make(map[string]int)
	for i, es := range embedScores {
		embedRanks[es.chunkID] = i + 1
	}

	// 3. Combine using RRF formula: 1 / (k + rank)
	rrfScores := make(map[string]float64)
	k := float64(s.rrfK)

	// Add BM25 contribution
	for chunkID, rank := range bm25Ranks {
		rrfScores[chunkID] += 1.0 / (k + float64(rank))
	}

	// Add Embedding contribution
	for chunkID, rank := range embedRanks {
		rrfScores[chunkID] += 1.0 / (k + float64(rank))
	}

	// 4. Convert back to scoredChunks
	results := make([]scoredChunk, 0, len(rrfScores))
	for _, chunk := range idx.Chunks {
		if score, ok := rrfScores[chunk.ChunkID]; ok {
			results = append(results, scoredChunk{chunk: chunk, score: score})
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	return results
}

// cosineSimilarity calculates cosine similarity between two vectors.
// Returns a value between -1 (opposite) and 1 (identical).
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
