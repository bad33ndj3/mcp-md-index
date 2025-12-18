// Package mcp provides MCP tool handlers for the documentation server.
// These handlers parse MCP request arguments and delegate to the Indexer.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bad33ndj3/mcp-md-index/internal/indexer"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LoadArgs defines the arguments for the docs_load tool.
type LoadArgs struct {
	Path string `json:"path" jsonschema_description:"Path to a local markdown file (e.g. docs/nats.md)"`
}

// QueryArgs defines the arguments for the docs_query tool.
type QueryArgs struct {
	DocID     string `json:"doc_id,omitempty" jsonschema_description:"DocID returned from docs_load (optional if path is provided)"`
	Path      string `json:"path,omitempty" jsonschema_description:"Path to the markdown file (used to derive doc_id if doc_id omitted)"`
	Prompt    string `json:"prompt" jsonschema_description:"Short query prompt (e.g. 'consumer')"`
	MaxTokens int    `json:"max_tokens,omitempty" jsonschema_description:"Approx max tokens to return (default 500)"`
}

// Handlers wraps the indexer and provides MCP tool handlers.
type Handlers struct {
	indexer *indexer.Indexer
	logger  *slog.Logger
}

// NewHandlers creates handlers with the given indexer and logger.
func NewHandlers(idx *indexer.Indexer, logger *slog.Logger) *Handlers {
	return &Handlers{indexer: idx, logger: logger}
}

// DocsLoad handles the docs_load tool call.
// It loads and indexes a markdown file, caching it for future queries.
func (h *Handlers) DocsLoad(ctx context.Context, req *mcp.CallToolRequest, args LoadArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		h.logger.Error("docs_load: path is required")
		return nil, nil, fmt.Errorf("path is required")
	}

	h.logger.Debug("docs_load: loading file", "path", args.Path)

	result, err := h.indexer.Load(args.Path)
	if err != nil {
		h.logger.Error("docs_load: failed to load", "path", args.Path, "error", err)
		return nil, nil, err
	}

	h.logger.Info("docs_load: success",
		"path", args.Path,
		"doc_id", result.DocID,
		"chunks", result.NumChunks,
		"from_cache", result.FromCache,
	)

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
		h.logger.Error("docs_query: doc_id or path is required")
		return nil, nil, fmt.Errorf("doc_id or path is required")
	}

	if prompt == "" {
		h.logger.Error("docs_query: prompt is required")
		return nil, nil, fmt.Errorf("prompt is required")
	}

	h.logger.Debug("docs_query: searching",
		"doc_id", docID,
		"path", path,
		"prompt", prompt,
		"max_tokens", args.MaxTokens,
	)

	answer, err := h.indexer.Query(docID, path, prompt, args.MaxTokens)
	if err != nil {
		h.logger.Error("docs_query: failed", "error", err)
		return nil, nil, err
	}

	h.logger.Info("docs_query: success",
		"prompt", prompt,
		"answer_length", len(answer),
	)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: answer}},
	}, nil, nil
}
