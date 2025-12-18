// Package main is the entry point for the mcp-md-index server.
// It wires together all dependencies and starts the MCP server.
//
// This file is intentionally minimal - all business logic lives in internal/.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/cache"
	"github.com/bad33ndj3/mcp-md-index/internal/indexer"
	mcphandlers "github.com/bad33ndj3/mcp-md-index/internal/mcp"
	"github.com/bad33ndj3/mcp-md-index/internal/parser"
	"github.com/bad33ndj3/mcp-md-index/internal/search"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "mcp-md-index"
	serverVersion = "v0.2.0"
	cacheDir      = ".mcp-md-index-cache"
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

	// --- 0. Setup file-based debug logger ---

	logger, logFile, err := setupLogger(cacheDir)
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
		"cache_dir", cacheDir,
	)

	// --- 1. Create all dependencies ---

	// Cache: stores parsed indexes in memory and on disk
	fileCache, err := cache.NewFileCache(cacheDir)
	if err != nil {
		logger.Error("failed to create cache", "error", err)
		log.Fatalf("Failed to create cache: %v", err)
	}

	// Parser: splits markdown into searchable chunks
	mdParser := parser.NewMarkdownParser()

	// Searcher: ranks chunks using BM25 algorithm
	bm25Searcher := search.NewBM25Searcher()

	// File reader: reads from the actual filesystem
	fileReader := indexer.OSFileReader{}

	// Clock: uses real system time
	clock := indexer.RealClock{}

	// --- 2. Wire up the indexer (orchestrator) ---

	idx := indexer.New(fileCache, mdParser, bm25Searcher, fileReader, clock)

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
		Name:        "docs_query",
		Description: "Query an indexed markdown document and return token-bounded, source-linked excerpts for a short prompt.",
	}, handlers.DocsQuery)

	logger.Info("server ready, waiting for requests")

	// --- 5. Run the server ---

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		logger.Error("server error", "error", err)
		log.Fatal(err)
	}
}
