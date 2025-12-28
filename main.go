// Package main is the entry point for the mcp-md-index server.
// It wires together all dependencies and starts the MCP server.
//
// This file is intentionally minimal - all business logic lives in internal/.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/cache"
	"github.com/bad33ndj3/mcp-md-index/internal/embedding"
	"github.com/bad33ndj3/mcp-md-index/internal/fetcher"
	"github.com/bad33ndj3/mcp-md-index/internal/indexer"
	mcphandlers "github.com/bad33ndj3/mcp-md-index/internal/mcp"
	"github.com/bad33ndj3/mcp-md-index/internal/parser"
	"github.com/bad33ndj3/mcp-md-index/internal/search"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName      = "mcp-md-index"
	serverVersion   = "v0.2.0"
	defaultCacheDir = ".mcp-cache"
)

// setupLogger creates an slog logger that writes to a debug file in the cache directory.
// File format: debug-YYYY-MM-DD.txt
func setupLogger(cacheDir string) (*slog.Logger, *os.File, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create cache dir: %w", err)
	}

	// Create debug log file with date
	date := time.Now().Format("2006-01-02")
	logPath := filepath.Join(cacheDir, fmt.Sprintf("debug-%s.txt", date))

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}

	// Create text handler that writes to file
	handler := slog.NewTextHandler(file, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := slog.New(handler)
	return logger, file, nil
}

func main() {
	// IMPORTANT: MCP stdio servers must log to stderr only (for standard log package).
	log.SetOutput(os.Stderr)

	// --- 0. Parse flags ---
	cacheDir := flag.String("cache-dir", defaultCacheDir, "Directory for cache and log files")
	experimentalEmbeddings := flag.Bool("experimental-embeddings", false,
		"Enable Ollama-based semantic search (experimental, non-blocking)")
	ollamaHost := flag.String("ollama-host", "http://localhost:11434",
		"Ollama server URL for embeddings")
	ollamaModel := flag.String("ollama-model", "nomic-embed-text",
		"Ollama embedding model to use")

	// Hybrid search flags
	fusionMethod := flag.String("hybrid-fusion-method", search.FusionMethodRRF,
		"Fusion method for hybrid search: 'rrf' or 'weighted'")
	bm25Weight := flag.Float64("hybrid-bm25-weight", 0.3,
		"BM25 weight for weighted fusion (0.0-1.0)")
	embedWeight := flag.Float64("hybrid-embed-weight", 0.7,
		"Embedding weight for weighted fusion (0.0-1.0)")
	rrfK := flag.Int("hybrid-rrf-k", search.DefaultRRFK,
		"K constant for Reciprocal Rank Fusion")
	maxConcurrent := flag.Int("max-concurrent-embeddings", 2,
		"Maximum number of concurrent embedding tasks")

	flag.Parse()

	// --- 1. Setup file-based debug logger ---

	logger, logFile, err := setupLogger(*cacheDir)
	if err != nil {
		log.Printf("Warning: failed to setup file logger: %v", err)
		// Fallback to a no-op logger
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	} else {
		defer logFile.Close()
	}

	logger.Info("server starting",
		"name", serverName,
		"version", serverVersion,
		"cache_dir", *cacheDir,
	)

	// --- 1. Create all dependencies ---

	// Cache: stores parsed indexes in memory and on disk
	fileCache, err := cache.NewFileCache(*cacheDir)
	if err != nil {
		logger.Error("failed to create cache", "error", err)
		log.Fatalf("Failed to create cache: %v", err)
	}

	// Parser: splits markdown into searchable chunks
	mdParser := parser.NewMarkdownParser()

	// --- 2. Setup Searcher (BM25 or Hybrid) ---
	var searcher search.Searcher
	var embedder embedding.Embedder
	var embedStatus *embedding.Status

	if *experimentalEmbeddings {
		embedCfg := embedding.Config{
			Host:  *ollamaHost,
			Model: *ollamaModel,
		}
		var err error
		embedder, err = embedding.NewOllamaEmbedder(embedCfg)
		if err != nil {
			logger.Warn("failed to create embedder, using BM25 only", "error", err)
			searcher = search.NewBM25Searcher()
		} else {
			embedStatus = embedding.NewStatus()
			hybrid := search.NewHybridSearcher(embedder, embedStatus)
			hybrid.WithFusionMethod(*fusionMethod, *bm25Weight, *embedWeight, *rrfK)
			searcher = hybrid

			logger.Info("experimental embeddings enabled (async)",
				"model", *ollamaModel,
				"host", *ollamaHost,
				"fusion", *fusionMethod)
		}
	} else {
		searcher = search.NewBM25Searcher()
	}

	// File reader: reads from the actual filesystem
	fileReader := indexer.OSFileReader{}

	// Clock: uses real system time
	clock := indexer.RealClock{}

	// Site fetcher: converts websites to markdown
	siteFetcher := fetcher.NewHTTPFetcher()

	// --- 3. Wire up the indexer (orchestrator) ---

	var idxOpts []indexer.Option
	idxOpts = append(idxOpts, indexer.WithLogger(logger))
	if embedder != nil {
		idxOpts = append(idxOpts, indexer.WithEmbedder(embedder, embedStatus))
		idxOpts = append(idxOpts, indexer.WithMaxConcurrentEmbeddings(*maxConcurrent))
	}

	idx := indexer.New(fileCache, mdParser, searcher, fileReader, clock, siteFetcher, idxOpts...)

	// --- 3. Create MCP handlers ---

	handlers := mcphandlers.NewHandlers(idx, logger)

	// --- 4. Create and configure the MCP server ---

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: "Use docs_load to index a markdown file once (cached), then docs_query to fetch token-bounded, source-linked excerpts.",
	})

	// Register tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "docs_load",
		Description: "Load + index a markdown file and cache it locally for fast subsequent queries.",
	}, handlers.DocsLoad)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "docs_load_glob",
		Description: "Load multiple markdown files matching a glob pattern (e.g. 'docs/**/*.md'). Faster than calling docs_load repeatedly.",
	}, handlers.DocsLoadGlob)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "docs_query",
		Description: "Query indexed documents. If doc_id/path omitted, searches ALL loaded docs. Returns token-bounded, source-linked excerpts.",
	}, handlers.DocsQuery)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "site_loads",
		Description: "Fetch multiple website URLs, convert HTML to markdown, and cache them for querying.",
	}, handlers.SiteLoads)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_repository",
		Description: "Index a source repository with safe defaults (excludes vendor, gen, test files). Use this for loading codebases.",
	}, handlers.ReadRepository)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "indexing_status",
		Description: "Check the progress of background indexing (queue depth, embedded count, etc).",
	}, handlers.IndexingStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "docs_list",
		Description: "List all currently cached documents (from docs_load or site_load). Returns doc_id, path, and chunk count.",
	}, handlers.DocsList)

	logger.Info("server ready, waiting for requests")

	// --- 5. Run the server ---

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		logger.Error("server error", "error", err)
		log.Fatal(err)
	}
}
