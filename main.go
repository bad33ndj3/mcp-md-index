// Package main is the entry point for the mcp-mdx server.
// It wires together all dependencies and starts the MCP server.
//
// This file is intentionally minimal - all business logic lives in internal/.
package main

import (
	"context"
	"log"
	"os"

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
	cacheDir      = ".mcp-mdx-cache"
)

func main() {
	// IMPORTANT: MCP stdio servers must log to stderr only.
	log.SetOutput(os.Stderr)

	// --- 1. Create all dependencies ---

	// Cache: stores parsed indexes in memory and on disk
	fileCache, err := cache.NewFileCache(cacheDir)
	if err != nil {
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

	handlers := mcphandlers.NewHandlers(idx)

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

	// --- 5. Run the server ---

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
