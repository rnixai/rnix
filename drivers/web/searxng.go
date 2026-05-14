package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/rnixai/rnix/internal/types"
)

// searxngBackend talks to a self-hosted SearXNG instance using its JSON API.
//
// Behavior preserved from Story 33.1:
//   - allowedDomains are appended to the query as `site:` operators
//     (SearXNG's preferred include filter)
//   - blockedDomains are stripped client-side after the response is parsed
type searxngBackend struct {
	client  HTTPClient
	baseURL string
}

// NewSearxngBackend builds a SearXNG backend bound to the given base URL.
// Returns nil when baseURL is empty so the registry can skip registration with
// a single warning rather than crashing later in Search.
func NewSearxngBackend(client HTTPClient, baseURL string) SearchBackend {
	if baseURL == "" {
		return nil
	}
	return &searxngBackend{client: client, baseURL: baseURL}
}

func (s *searxngBackend) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	baseURL := s.baseURL
	if baseURL == "" {
		return nil, &types.DriverError{
			Op:     "Search",
			Device: "/dev/web",
			Err:    fmt.Errorf("searxng: search backend not configured: set RNIX_SEARCH_URL environment variable"),
			Code:   types.ErrServiceUnavailable,
		}
	}

	var sb strings.Builder
	sb.WriteString(params.Query)
	for _, d := range params.AllowedDomains {
		sb.WriteString(" site:")
		sb.WriteString(d)
	}
	q := sb.String()

	searchURL := fmt.Sprintf("%s/search?q=%s&format=json", strings.TrimRight(baseURL, "/"), url.QueryEscape(q))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, wrapBackendError("searxng", "Search", fmt.Errorf("creating request: %w", err), types.ErrDriver)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, wrapBackendError("searxng", "Search", fmt.Errorf("request failed: %w", err), types.ErrServiceUnavailable)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, classifyHTTPError("searxng", resp.StatusCode, body)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, wrapBackendError("searxng", "Search", fmt.Errorf("reading response: %w", err), types.ErrDriver)
	}

	var searxResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &searxResp); err != nil {
		return nil, wrapBackendError("searxng", "Search", fmt.Errorf("parsing response: %w", err), types.ErrDriver)
	}

	blocked := make(map[string]bool, len(params.BlockedDomains))
	for _, d := range params.BlockedDomains {
		blocked[strings.ToLower(d)] = true
	}

	max := params.MaxResults
	results := make([]SearchResult, 0, len(searxResp.Results))
	for _, r := range searxResp.Results {
		if len(blocked) > 0 {
			if u, err := url.Parse(r.URL); err == nil {
				if blocked[strings.ToLower(u.Host)] {
					continue
				}
			}
		}
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
		if max > 0 && len(results) >= max {
			break
		}
	}
	return results, nil
}
