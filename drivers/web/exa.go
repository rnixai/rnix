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

const exaEndpoint = "https://api.exa.ai/search"

// exaBackend talks to the Exa search API.
// Docs: https://docs.exa.ai/reference/search
//
// Field names differ from Tavily (camelCase vs snake_case) — see request body
// construction in Search.
type exaBackend struct {
	client     HTTPClient
	apiKey     string
	numResults int
}

// NewExaBackend constructs an Exa backend. apiKey must be non-empty; the
// registry-build path skips registration when the API key env var is unset.
func NewExaBackend(client HTTPClient, apiKey string, numResults int) SearchBackend {
	if numResults <= 0 {
		numResults = DefaultMaxResults
	}
	return &exaBackend{
		client:     client,
		apiKey:     apiKey,
		numResults: numResults,
	}
}

func (e *exaBackend) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	num := params.MaxResults
	if num <= 0 {
		num = e.numResults
	}

	payload := map[string]any{
		"query":      params.Query,
		"numResults": num,
		// contents.text=true asks Exa to include extracted text in each result;
		// without it, Snippet would be empty (only title/url/score returned).
		"contents": map[string]any{"text": true},
	}
	if len(params.AllowedDomains) > 0 {
		payload["includeDomains"] = params.AllowedDomains
	}
	if len(params.BlockedDomains) > 0 {
		payload["excludeDomains"] = params.BlockedDomains
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, wrapBackendError("exa", "Search", fmt.Errorf("marshal request: %w", err), types.ErrDriver)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exaEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, wrapBackendError("exa", "Search", fmt.Errorf("creating request: %w", err), types.ErrDriver)
	}
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, wrapBackendError("exa", "Search", fmt.Errorf("request failed: %w", err), types.ErrServiceUnavailable)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, wrapBackendError("exa", "Search", fmt.Errorf("reading response: %w", readErr), types.ErrDriver)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHTTPError("exa", resp.StatusCode, respBody)
	}

	var parsed struct {
		Results []struct {
			Title         string  `json:"title"`
			URL           string  `json:"url"`
			Score         float64 `json:"score"`
			PublishedDate string  `json:"publishedDate"`
			Text          string  `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, wrapBackendError("exa", "Search", fmt.Errorf("parsing response: %w", err), types.ErrDriver)
	}

	results := make([]SearchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Text,
		})
	}
	return results, nil
}
