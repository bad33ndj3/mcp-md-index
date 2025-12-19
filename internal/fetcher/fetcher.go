// Package fetcher provides HTTP fetching and HTML-to-Markdown conversion.
// It abstracts external URL fetching for testability.
package fetcher

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

// Fetcher abstracts URL fetching and conversion for testability.
type Fetcher interface {
	// FetchAsMarkdown fetches a URL and converts it to markdown.
	// Returns the markdown content and any error encountered.
	FetchAsMarkdown(urlStr string) (markdown string, err error)
}

// HTTPFetcher is the production implementation using real HTTP requests.
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher creates a new HTTPFetcher with sensible defaults.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchAsMarkdown fetches a URL and converts HTML to markdown.
func (f *HTTPFetcher) FetchAsMarkdown(urlStr string) (string, error) {
	// Parse URL to extract domain for relative link resolution
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}

	// Build base URL for relative link resolution
	domain := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// Fetch the page
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "mcp-md-index/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	// Convert HTML to markdown with domain for absolute URLs
	markdown, err := htmltomarkdown.ConvertString(
		string(body),
		converter.WithDomain(domain),
	)
	if err != nil {
		return "", fmt.Errorf("convert to markdown: %w", err)
	}

	return markdown, nil
}
