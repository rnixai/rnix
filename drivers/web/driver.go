// Package web implements the /dev/web VFS device for web access (fetch + search).
package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const (
	// maxResultChars is the maximum character count for web_fetch results.
	maxResultChars = 100000
	// cacheTTL is how long fetched pages are cached.
	cacheTTL = 15 * time.Minute
	// httpTimeout is the HTTP client request timeout.
	httpTimeout = 30 * time.Second
)

// cacheEntry holds a cached page result with expiry.
type cacheEntry struct {
	data      string
	expiresAt time.Time
}

// HTTPClient abstracts HTTP requests for testability.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SearchBackend abstracts web search for testability.
type SearchBackend interface {
	Search(ctx context.Context, query string, allowedDomains, blockedDomains []string) ([]SearchResult, error)
}

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// HTMLConverter abstracts HTML-to-Markdown conversion for testability.
type HTMLConverter interface {
	Convert(htmlContent string) (string, error)
}

// WebDriver manages web access operations.
type WebDriver struct {
	httpClient HTTPClient
	converter  HTMLConverter
	search     SearchBackend

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*WebDriver)(nil)

// ToolDefs returns the tool definitions for the web device.
func (d *WebDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "web_fetch",
			Description:       loadPrompt("web_fetch"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			ShouldDefer:       true,
			SearchHint:        "web fetch url page content",
			MaxResultTokens:   100000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "The URL to fetch",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Optional extraction prompt prepended to the result",
					},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:              "web_search",
			Description:       loadPrompt("web_search"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			ShouldDefer:       true,
			SearchHint:        "web search query internet",
			MaxResultTokens:   100000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query string",
					},
					"allowed_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Domains to restrict results to",
					},
					"blocked_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Domains to exclude from results",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// DriverOpts holds configuration for WebDriver.
type DriverOpts struct {
	HTTPClient HTTPClient
	Converter  HTMLConverter
	Search     SearchBackend
}

// NewDriver creates a WebDriver with default production configuration.
func NewDriver() *WebDriver {
	return &WebDriver{
		httpClient: defaultHTTPClient(),
		converter:  &simpleHTMLConverter{},
		search:     &searxngBackend{client: defaultHTTPClient()},
		cache:      make(map[string]cacheEntry),
	}
}

// NewDriverWithOptions creates a WebDriver with custom configuration.
func NewDriverWithOptions(opts DriverOpts) *WebDriver {
	d := &WebDriver{
		httpClient: opts.HTTPClient,
		converter:  opts.Converter,
		search:     opts.Search,
		cache:      make(map[string]cacheEntry),
	}
	if d.httpClient == nil {
		d.httpClient = defaultHTTPClient()
	}
	if d.converter == nil {
		d.converter = &simpleHTMLConverter{}
	}
	if d.search == nil {
		d.search = &searxngBackend{client: d.httpClient}
	}
	return d
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects but track cross-domain
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

// getCached returns cached content for a URL if still valid.
func (d *WebDriver) getCached(rawURL string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.cache[rawURL]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(d.cache, rawURL)
		}
		return "", false
	}
	return entry.data, true
}

// setCache stores content in the cache.
func (d *WebDriver) setCache(rawURL, content string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache[rawURL] = cacheEntry{data: content, expiresAt: time.Now().Add(cacheTTL)}
}

// fetch retrieves a URL, converting HTML to Markdown.
func (d *WebDriver) fetch(ctx context.Context, rawURL, prompt string) (string, error) {
	// Validate URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid URL: missing scheme or host")
	}

	// Auto-upgrade HTTP → HTTPS
	fetchURL := rawURL
	if parsed.Scheme == "http" {
		httpsURL := "https" + rawURL[4:]
		fetchURL = httpsURL
	}

	// Check cache
	if cached, ok := d.getCached(fetchURL); ok {
		return d.formatResult(cached, prompt), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Rnix/1.0 (Agent OS)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		// If HTTPS upgrade failed, retry with original HTTP URL
		if parsed.Scheme == "http" && fetchURL != rawURL {
			req2, err2 := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err2 != nil {
				return "", fmt.Errorf("fetch failed (HTTPS): %w; HTTP fallback failed: %w", err, err2)
			}
			req2.Header.Set("User-Agent", "Rnix/1.0 (Agent OS)")
			resp, err = d.httpClient.Do(req2)
			if err != nil {
				return "", fmt.Errorf("fetch failed: %w", err)
			}
			fetchURL = rawURL
		} else {
			return "", fmt.Errorf("fetch failed: %w", err)
		}
	}
	defer resp.Body.Close()

	// Check for cross-domain redirect
	finalURL := resp.Request.URL.String()
	if isCrossDomainRedirect(fetchURL, finalURL) {
		return fmt.Sprintf("Cross-domain redirect detected.\nOriginal: %s\nRedirected to: %s\n\nTo access the redirected content, call web_fetch with the new URL.", fetchURL, finalURL), nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResultChars*4)))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	content := string(body)

	// Convert HTML to markdown if content looks like HTML
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "html") || looksLikeHTML(content) {
		md, err := d.converter.Convert(content)
		if err == nil {
			content = md
		}
	}

	// Truncate to maxResultChars
	truncated, _ := rnixctx.TruncateResult(content, maxResultChars)

	// Cache the result
	d.setCache(fetchURL, truncated)

	return d.formatResult(truncated, prompt), nil
}

func (d *WebDriver) formatResult(content, prompt string) string {
	if prompt != "" {
		return fmt.Sprintf("[Extraction prompt]: %s\n\n---\n\n%s", prompt, content)
	}
	return content
}

// isCrossDomainRedirect checks if a redirect crosses domain boundaries.
func isCrossDomainRedirect(originalURL, finalURL string) bool {
	orig, err1 := url.Parse(originalURL)
	final, err2 := url.Parse(finalURL)
	if err1 != nil || err2 != nil {
		return false
	}
	if orig.Host == "" || final.Host == "" {
		return false
	}
	return orig.Host != final.Host
}

// looksLikeHTML does a simple check for HTML content.
func looksLikeHTML(content string) bool {
	trimmed := strings.TrimSpace(content)
	lower := strings.ToLower(trimmed[:min(len(trimmed), 500)])
	return strings.Contains(lower, "<!doctype html") || strings.Contains(lower, "<html") || strings.Contains(lower, "<head")
}

// WebFile implements vfs.VFSFile for web operations via write-then-read semantics.
type WebFile struct {
	driver     *WebDriver
	devicePath string
	response   []byte
	offset     int
	closed     bool
}

// Write accepts a JSON request, dispatches to fetch or search, and buffers the result.
func (f *WebFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("web file closed"), Code: types.ErrDriver}
	}

	var req struct {
		URL            string   `json:"url"`
		Prompt         string   `json:"prompt"`
		Query          string   `json:"query"`
		AllowedDomains []string `json:"allowed_domains"`
		BlockedDomains []string `json:"blocked_domains"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid JSON input: %w", err), Code: types.ErrInvalid}
	}

	var result string
	var err error

	switch {
	case req.URL != "":
		// web_fetch operation
		result, err = f.driver.fetch(ctx, req.URL, req.Prompt)
	case req.Query != "":
		// web_search operation
		results, searchErr := f.driver.search.Search(ctx, req.Query, req.AllowedDomains, req.BlockedDomains)
		if searchErr != nil {
			err = searchErr
		} else {
			jsonData, _ := json.MarshalIndent(results, "", "  ")
			result = string(jsonData)
		}
	default:
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("must specify either 'url' (for fetch) or 'query' (for search)"), Code: types.ErrInvalid}
	}

	if err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrDriver}
	}

	f.response = []byte(result)
	f.offset = 0
	return nil
}

// Read returns buffered response data up to the requested length.
func (f *WebFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("web file closed"), Code: types.ErrDriver}
	}
	if f.response == nil {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("no data available: write a request first"), Code: types.ErrDriver}
	}

	remaining := f.response[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}

	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}

	out := make([]byte, length)
	copy(out, remaining[:length])
	f.offset += length
	return out, nil
}

// Close marks the file as closed and releases buffers.
func (f *WebFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "Close", Device: f.devicePath, Err: fmt.Errorf("web file already closed"), Code: types.ErrDriver}
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

// Stat returns metadata about this web device file.
func (f *WebFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, &types.DriverError{Op: "Stat", Device: f.devicePath, Err: fmt.Errorf("web file closed"), Code: types.ErrDriver}
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		Size:       int64(len(f.response)),
		IsDevice:   true,
		DevicePath: "/dev/web",
	}, nil
}

// FileFactory returns a VFSFileFactory that creates WebFile instances.
func FileFactory(driver *WebDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &WebFile{
			driver:     driver,
			devicePath: "/dev/web" + subpath,
		}, nil
	}
}

// --- simpleHTMLConverter: basic HTML-to-Markdown ---

type simpleHTMLConverter struct{}

func (c *simpleHTMLConverter) Convert(htmlContent string) (string, error) {
	// Simple HTML-to-text conversion: strip tags and decode entities.
	// For production quality, a full converter (e.g., html-to-markdown lib) would be used,
	// but we avoid adding a dependency for now and keep the interface mockable.
	result := htmlContent

	// Remove script and style blocks
	result = removeTagBlock(result, "script")
	result = removeTagBlock(result, "style")

	// Convert common block elements to newlines
	for _, tag := range []string{"p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr"} {
		result = strings.ReplaceAll(result, "<"+tag+">", "\n")
		result = strings.ReplaceAll(result, "<"+tag+" ", "\n<"+tag+" ")
		result = strings.ReplaceAll(result, "</"+tag+">", "\n")
	}

	// Strip remaining tags
	var sb strings.Builder
	inTag := false
	for _, ch := range result {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			sb.WriteRune(ch)
		}
	}
	result = sb.String()

	// Decode common HTML entities
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&nbsp;", " ")

	// Collapse multiple blank lines
	lines := strings.Split(result, "\n")
	var cleaned []string
	prevEmpty := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevEmpty {
				cleaned = append(cleaned, "")
			}
			prevEmpty = true
		} else {
			cleaned = append(cleaned, trimmed)
			prevEmpty = false
		}
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n")), nil
}

// removeTagBlock removes everything between <tag...> and </tag>.
func removeTagBlock(s, tag string) string {
	lower := strings.ToLower(s)
	for {
		start := strings.Index(lower, "<"+tag)
		if start == -1 {
			break
		}
		end := strings.Index(lower[start:], "</"+tag+">")
		if end == -1 {
			break
		}
		endAbs := start + end + len("</"+tag+">")
		s = s[:start] + s[endAbs:]
		lower = strings.ToLower(s)
	}
	return s
}

// --- SearXNG search backend ---

type searxngBackend struct {
	client  HTTPClient
	baseURL string // override via RNIX_SEARCH_URL env var
}

func (s *searxngBackend) Search(ctx context.Context, query string, allowedDomains, blockedDomains []string) ([]SearchResult, error) {
	baseURL := s.baseURL
	if baseURL == "" {
		return nil, &types.DriverError{
			Op:     "Search",
			Device: "/dev/web",
			Err:    fmt.Errorf("search backend not configured: set RNIX_SEARCH_URL environment variable"),
			Code:   types.ErrServiceUnavailable,
		}
	}

	// Build query with domain filters
	var sb strings.Builder
	sb.WriteString(query)
	for _, d := range allowedDomains {
		sb.WriteString(" site:")
		sb.WriteString(d)
	}
	q := sb.String()

	searchURL := fmt.Sprintf("%s/search?q=%s&format=json", strings.TrimRight(baseURL, "/"), url.QueryEscape(q))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, &types.DriverError{
			Op:     "Search",
			Device: "/dev/web",
			Err:    fmt.Errorf("search request failed: %w", err),
			Code:   types.ErrServiceUnavailable,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &types.DriverError{
			Op:     "Search",
			Device: "/dev/web",
			Err:    fmt.Errorf("search backend returned HTTP %d", resp.StatusCode),
			Code:   types.ErrServiceUnavailable,
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading search response: %w", err)
	}

	var searxResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &searxResp); err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}

	var results []SearchResult
	blocked := make(map[string]bool)
	for _, d := range blockedDomains {
		blocked[strings.ToLower(d)] = true
	}

	for _, r := range searxResp.Results {
		// Apply blocked_domains filter
		if len(blocked) > 0 {
			u, err := url.Parse(r.URL)
			if err == nil && blocked[strings.ToLower(u.Host)] {
				continue
			}
		}
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}

	return results, nil
}
