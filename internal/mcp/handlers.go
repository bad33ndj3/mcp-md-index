// Package mcp provides MCP tool handlers for the documentation server.
// These handlers parse MCP request arguments and delegate to the Indexer.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
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

// SiteLoadsArgs defines the arguments for the site_loads tool.
type SiteLoadsArgs struct {
	URLs  []string `json:"urls" jsonschema_description:"URLs of websites to fetch and convert to markdown"`
	Force bool     `json:"force,omitempty" jsonschema_description:"Force re-fetch even if cached (default: false)"`
}

// LoadGlobArgs defines the arguments for the docs_load_glob tool.
type LoadGlobArgs struct {
	Pattern string `json:"pattern" jsonschema_description:"Glob pattern to match markdown files (e.g. 'docs/**/*.md', '*.md')"`
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

// DocsLoadGlob handles the docs_load_glob tool call.
// It loads multiple markdown files matching a glob pattern.
func (h *Handlers) DocsLoadGlob(ctx context.Context, req *mcp.CallToolRequest, args LoadGlobArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Pattern) == "" {
		h.logger.Error("docs_load_glob: pattern is required")
		return nil, nil, fmt.Errorf("pattern is required")
	}

	h.logger.Debug("docs_load_glob: loading files", "pattern", args.Pattern)

	result, err := h.indexer.LoadGlob(args.Pattern)
	if err != nil {
		h.logger.Error("docs_load_glob: failed", "pattern", args.Pattern, "error", err)
		return nil, nil, err
	}

	h.logger.Info("docs_load_glob: success",
		"pattern", args.Pattern,
		"loaded", result.Loaded,
		"cached", result.Cached,
		"failed", result.Failed,
	)

	// Keep response concise - just summary stats
	totalChunks := 0
	for _, r := range result.Results {
		totalChunks += r.NumChunks
	}

	msg := fmt.Sprintf("Loaded %d files (%d cached), %d chunks total", result.Loaded, result.Cached, totalChunks)
	if result.Failed > 0 {
		msg += fmt.Sprintf(", %d failed", result.Failed)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// DocsQuery handles the docs_query tool call.
// It searches an indexed document and returns token-bounded excerpts.
// If no doc_id or path is provided, searches across all loaded documents.
func (h *Handlers) DocsQuery(ctx context.Context, req *mcp.CallToolRequest, args QueryArgs) (*mcp.CallToolResult, any, error) {
	docID := strings.TrimSpace(args.DocID)
	path := strings.TrimSpace(args.Path)
	prompt := strings.TrimSpace(args.Prompt)

	if prompt == "" {
		h.logger.Error("docs_query: prompt is required")
		return nil, nil, fmt.Errorf("prompt is required")
	}

	var answer string
	var err error

	// If no doc_id or path, search all documents
	if docID == "" && path == "" {
		h.logger.Debug("docs_query: searching all documents",
			"prompt", prompt,
			"max_tokens", args.MaxTokens,
		)
		answer, err = h.indexer.QueryAll(prompt, args.MaxTokens)
	} else {
		h.logger.Debug("docs_query: searching specific document",
			"doc_id", docID,
			"path", path,
			"prompt", prompt,
			"max_tokens", args.MaxTokens,
		)
		answer, err = h.indexer.Query(docID, path, prompt, args.MaxTokens)
	}

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

// SiteLoads handles the site_loads tool call.
// It fetches multiple websites, converts HTML to markdown, and caches them for future queries.
func (h *Handlers) SiteLoads(ctx context.Context, req *mcp.CallToolRequest, args SiteLoadsArgs) (*mcp.CallToolResult, any, error) {
	if len(args.URLs) == 0 {
		h.logger.Error("site_loads: urls is required")
		return nil, nil, fmt.Errorf("urls is required (provide at least one URL)")
	}

	h.logger.Debug("site_loads: fetching sites", "count", len(args.URLs), "force", args.Force)

	var sb strings.Builder
	loaded, cached, failed := 0, 0, 0

	for _, url := range args.URLs {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}

		result, err := h.indexer.LoadSite(url, args.Force)
		if err != nil {
			h.logger.Error("site_loads: failed to load", "url", url, "error", err)
			failed++
			sb.WriteString(fmt.Sprintf("- FAILED: %s (%v)\n", url, err))
			continue
		}

		loaded++
		if result.FromCache {
			cached++
		}
		sb.WriteString(fmt.Sprintf("- %s (chunks: %d)\n", url, result.NumChunks))
	}

	h.logger.Info("site_loads: complete",
		"loaded", loaded,
		"cached", cached,
		"failed", failed,
	)

	header := fmt.Sprintf("Loaded %d sites (%d from cache, %d failed)\n\n", loaded, cached, failed)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: header + sb.String()}},
	}, nil, nil
}

// ReadRepositoryArgs defines the arguments for the read_repository tool.
type ReadRepositoryArgs struct {
	Path     string   `json:"path" jsonschema_description:"Root directory of the repository or service to index"`
	Excludes []string `json:"excludes,omitempty" jsonschema_description:"Glob patterns to exclude (defaults to vendor, gen, test files)"`
}

// ReadRepository handles the read_repository tool call.
// It indexes a codebase with safe defaults for ignoring build artifacts and dependencies.
func (h *Handlers) ReadRepository(ctx context.Context, req *mcp.CallToolRequest, args ReadRepositoryArgs) (*mcp.CallToolResult, any, error) {
	root := strings.TrimSpace(args.Path)
	if root == "" {
		h.logger.Error("read_repository: path is required")
		return nil, nil, fmt.Errorf("path is required")
	}

	h.logger.Debug("read_repository: scanning repo", "path", root)

	// Safe defaults for codebases
	excludes := []string{
		"**/vendor/**",
		"**/node_modules/**",
		"**/.git/**",
		"**/dist/**",
		"**/build/**",
		"**/*_test.go",
		"**/*.pb.go",
		"**/gen/**",
		"**/generated/**",
	}
	// Append user excludes
	excludes = append(excludes, args.Excludes...)

	// Glob pattern to find code files recursively
	// We'll target common source extensions.
	// Note: We use LoadGlob which now supports excludes (to be implemented next)
	// For now, we will construct a pattern that finds all files, and let LoadGlob filter them.
	pattern := filepath.Join(root, "**", "*")

	// Pass excludes via a new method or updated LoadGlob.
	// Since we haven't updated LoadGlob signature yet, we'll do it in two steps.
	// For now, let's assume we update LoadGlob to take options or we add a new LoadRepo method.
	// To keep it simple, we will call LoadGlobWithExcludes (which we will add to Indexer).

	// Async load
	err := h.indexer.LoadGlobAsync(pattern, excludes)
	if err != nil {
		h.logger.Error("read_repository: failed to start", "path", root, "error", err)
		return nil, nil, err
	}

	h.logger.Info("read_repository: started async", "path", root)

	msg := fmt.Sprintf("Started indexing repository at %s\n\nThis process runs in the background. Use 'docs_list' to check progress or see loaded files.", root)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// IndexingStatus returns the current progress of the indexing job.
func (h *Handlers) IndexingStatus(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
	stats := h.indexer.GetStatus()

	resp := map[string]any{
		"docs_count":     stats.DocsCount,
		"queue_length":   stats.QueueLength,
		"embedded_count": stats.EmbeddedCount,
		"active_workers": stats.ActiveWorkers,
		"status":         "idle",
	}

	if stats.QueueLength > 0 || stats.ActiveWorkers > 0 {
		resp["status"] = "indexing"
	}

	// Format as JSON
	jsonBytes, _ := json.MarshalIndent(resp, "", "  ")

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
	}, nil, nil
}

// DocsList handles the docs_list tool call.
// It returns a list of all currently cached documents.
func (h *Handlers) DocsList(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
	h.logger.Debug("docs_list: listing cached documents")

	docs := h.indexer.List()

	if len(docs) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "No documents currently loaded. Use docs_load, site_load, or read_repository first."}},
		}, nil, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Loaded documents: %d\n\n", len(docs)))

	// Cap the list to prevent context overflow if 6000 files are loaded
	const maxDisplay = 50
	for i, doc := range docs {
		if i >= maxDisplay {
			sb.WriteString(fmt.Sprintf("\n... and %d more files.", len(docs)-maxDisplay))
			break
		}
		sb.WriteString(fmt.Sprintf("- doc_id: %s\n", doc.DocID))
		if doc.SourceURL != "" {
			sb.WriteString(fmt.Sprintf("  url: %s\n", doc.SourceURL))
		}
		sb.WriteString(fmt.Sprintf("  path: %s\n", doc.Path))
		sb.WriteString(fmt.Sprintf("  chunks: %d\n", doc.NumChunks))
		// sb.WriteString(fmt.Sprintf("  indexed_at: %s\n\n", doc.IndexedAt.Format(time.RFC3339))) // Compact display
	}

	h.logger.Info("docs_list: success", "count", len(docs))

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil, nil
}
