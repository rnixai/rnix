package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rnixai/rnix/internal/types"
)

const tavilyEndpoint = "https://api.tavily.com/search"

// tavilyBackend talks to the Tavily search API.
// Docs: https://docs.tavily.com/documentation/api-reference/endpoint/search
type tavilyBackend struct {
	client      HTTPClient
	apiKey      string
	maxResults  int
	searchDepth string
}

// NewTavilyBackend constructs a Tavily backend. apiKey must be non-empty; the
// registry-build path already enforces that by skipping registration when the
// API key env var is unset.
func NewTavilyBackend(client HTTPClient, apiKey string, maxResults int, searchDepth string) SearchBackend {
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}
	return &tavilyBackend{
		client:      client,
		apiKey:      apiKey,
		maxResults:  maxResults,
		searchDepth: searchDepth,
	}
}

func (t *tavilyBackend) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = t.maxResults
	}

	payload := map[string]any{
		"query":       params.Query,
		"max_results": maxResults,
	}
	if t.searchDepth != "" {
		payload["search_depth"] = t.searchDepth
	}
	if len(params.AllowedDomains) > 0 {
		payload["include_domains"] = params.AllowedDomains
	}
	if len(params.BlockedDomains) > 0 {
		payload["exclude_domains"] = params.BlockedDomains
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, wrapBackendError("tavily", "Search", fmt.Errorf("marshal request: %w", err), types.ErrDriver)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilyEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, wrapBackendError("tavily", "Search", fmt.Errorf("creating request: %w", err), types.ErrDriver)
	}
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, wrapBackendError("tavily", "Search", fmt.Errorf("request failed: %w", err), types.ErrServiceUnavailable)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, wrapBackendError("tavily", "Search", fmt.Errorf("reading response: %w", readErr), types.ErrDriver)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHTTPError("tavily", resp.StatusCode, respBody)
	}

	var parsed struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, wrapBackendError("tavily", "Search", fmt.Errorf("parsing response: %w", err), types.ErrDriver)
	}

	results := make([]SearchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}
	return results, nil
}
