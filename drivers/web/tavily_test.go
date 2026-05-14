package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// helper: synthesize a Tavily HTTP response from a status code + body
func tavilyMockHTTP(status int, body string, capture func(req *http.Request)) HTTPClient {
	return newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		if capture != nil {
			capture(req)
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})
}

func TestTavilyBackend_Search_Success(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	respJSON := `{"results":[
        {"title":"Go","url":"https://go.dev","content":"Go is a programming language","score":0.9},
        {"title":"GoDoc","url":"https://pkg.go.dev","content":"docs","score":0.7}
    ]}`
	client := tavilyMockHTTP(200, respJSON, func(req *http.Request) {
		gotPath = req.URL.String()
		gotAuth = req.Header.Get("Authorization")
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &gotBody)
	})

	backend := NewTavilyBackend(client, "secret-key", 5, "basic")
	results, err := backend.Search(context.Background(), SearchParams{Query: "go programming"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if gotPath != tavilyEndpoint {
		t.Errorf("unexpected URL: %s", gotPath)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("unexpected Authorization header: %q", gotAuth)
	}
	if gotBody["query"] != "go programming" {
		t.Errorf("body query field wrong: %v", gotBody["query"])
	}
	if v, _ := gotBody["max_results"].(float64); int(v) != 5 {
		t.Errorf("body max_results wrong: %v", gotBody["max_results"])
	}
	if gotBody["search_depth"] != "basic" {
		t.Errorf("expected search_depth=basic, got %v", gotBody["search_depth"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Go" || results[0].Snippet != "Go is a programming language" {
		t.Errorf("first result mismatched: %+v", results[0])
	}
}

func TestTavilyBackend_Search_DomainFilters(t *testing.T) {
	var body map[string]any
	client := tavilyMockHTTP(200, `{"results":[]}`, func(req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &body)
	})
	backend := NewTavilyBackend(client, "k", 0, "")
	_, err := backend.Search(context.Background(), SearchParams{
		Query:          "rust",
		AllowedDomains: []string{"doc.rust-lang.org"},
		BlockedDomains: []string{"medium.com"},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	inc, ok := body["include_domains"].([]any)
	if !ok || len(inc) != 1 || inc[0] != "doc.rust-lang.org" {
		t.Errorf("expected include_domains=[doc.rust-lang.org], got %v", body["include_domains"])
	}
	exc, ok := body["exclude_domains"].([]any)
	if !ok || len(exc) != 1 || exc[0] != "medium.com" {
		t.Errorf("expected exclude_domains=[medium.com], got %v", body["exclude_domains"])
	}
}

func TestTavilyBackend_Search_HTTP401(t *testing.T) {
	client := tavilyMockHTTP(401, `{"error":"invalid key"}`, nil)
	backend := NewTavilyBackend(client, "bad-key", 5, "")
	_, err := backend.Search(context.Background(), SearchParams{Query: "x"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %v", de.Code)
	}
	if !strings.Contains(de.Error(), "tavily") {
		t.Errorf("error should include backend name, got: %v", de.Error())
	}
}

func TestTavilyBackend_Search_HTTP429(t *testing.T) {
	client := tavilyMockHTTP(429, `rate limited`, nil)
	backend := NewTavilyBackend(client, "k", 5, "")
	_, err := backend.Search(context.Background(), SearchParams{Query: "x"})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrServiceUnavailable {
		t.Errorf("expected ErrServiceUnavailable, got %v", de.Code)
	}
}

func TestTavilyBackend_Search_MalformedJSON(t *testing.T) {
	client := tavilyMockHTTP(200, `not-json`, nil)
	backend := NewTavilyBackend(client, "k", 5, "")
	_, err := backend.Search(context.Background(), SearchParams{Query: "x"})
	if err == nil {
		t.Fatal("expected parse error")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver for parse failure, got %v", de.Code)
	}
}

func TestTavilyBackend_Search_NetworkError(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	})
	backend := NewTavilyBackend(client, "k", 5, "")
	_, err := backend.Search(context.Background(), SearchParams{Query: "x"})
	if err == nil {
		t.Fatal("expected network error")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrServiceUnavailable {
		t.Errorf("expected ErrServiceUnavailable, got %v", de.Code)
	}
}

func TestTavilyBackend_MaxResults_RequestOverride(t *testing.T) {
	var body map[string]any
	client := tavilyMockHTTP(200, `{"results":[]}`, func(req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &body)
	})
	backend := NewTavilyBackend(client, "k", 5, "")
	_, _ = backend.Search(context.Background(), SearchParams{Query: "x", MaxResults: 15})
	if v, _ := body["max_results"].(float64); int(v) != 15 {
		t.Errorf("expected request override to win (15), got %v", body["max_results"])
	}
}
