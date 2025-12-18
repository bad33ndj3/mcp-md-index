// Package mcp provides MCP tool handlers for the documentation server.
// These handlers parse MCP request arguments and delegate to the Indexer.
package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/indexer"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LoadArgs defines the arguments for the docs_load tool.
type LoadArgs struct {
	Path string `json:"path" jsonschema:"required,description=Path to a local markdown file (e.g. docs/nats.md)"`
}

// QueryArgs defines the arguments for the docs_query tool.
type QueryArgs struct {
	DocID     string `json:"doc_id,omitempty" jsonschema:"description=DocID returned from docs_load (optional if path is provided)"`
	Path      string `json:"path,omitempty" jsonschema:"description=Path to the markdown file (used to derive doc_id if doc_id omitted)"`
	Prompt    string `json:"prompt" jsonschema:"required,description=Short query prompt (e.g. 'consumer')"`
	MaxTokens int    `json:"max_tokens,omitempty" jsonschema:"description=Approx max tokens to return (default 500)"`
}

// Handlers wraps the indexer and provides MCP tool handlers.
type Handlers struct {
	indexer *indexer.Indexer
}

// NewHandlers creates handlers with the given indexer.
func NewHandlers(idx *indexer.Indexer) *Handlers {
	return &Handlers{indexer: idx}
}

// DocsLoad handles the docs_load tool call.
// It loads and indexes a markdown file, caching it for future queries.
func (h *Handlers) DocsLoad(ctx context.Context, req *mcp.CallToolRequest, args LoadArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("path is required")
	}

	result, err := h.indexer.Load(args.Path)
	if err != nil {
		return nil, nil, err
	}

	var msg string
	if result.FromCache {
		msg = fmt.Sprintf("Loaded from cache.\n\ndoc_id: %s\npath: %s\nchunks: %d\nindexed_at: %s\n",
			result.DocID, result.Path, result.NumChunks, result.IndexedAt.Format(time.RFC3339))
	} else {
		msg = fmt.Sprintf("Indexed and cached.\n\ndoc_id: %s\npath: %s\nchunks: %d\n",
			result.DocID, result.Path, result.NumChunks)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// DocsQuery handles the docs_query tool call.
// It searches an indexed document and returns token-bounded excerpts.
func (h *Handlers) DocsQuery(ctx context.Context, req *mcp.CallToolRequest, args QueryArgs) (*mcp.CallToolResult, any, error) {
	docID := strings.TrimSpace(args.DocID)
	path := strings.TrimSpace(args.Path)
	prompt := strings.TrimSpace(args.Prompt)

	if docID == "" && path == "" {
		return nil, nil, fmt.Errorf("doc_id or path is required")
	}

	if prompt == "" {
		return nil, nil, fmt.Errorf("prompt is required")
	}

	answer, err := h.indexer.Query(docID, path, prompt, args.MaxTokens)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: answer}},
	}, nil, nil
}
