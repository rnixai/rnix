package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

func exaMockHTTP(status int, body string, capture func(req *http.Request)) HTTPClient {
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

func TestExaBackend_Search_Success(t *testing.T) {
	var gotPath string
	var gotKey string
	var gotBody map[string]any
	respJSON := `{"results":[
        {"title":"Exa","url":"https://exa.ai","score":0.5,"publishedDate":"2026-01-01","text":"Exa text"},
        {"title":"Docs","url":"https://docs.exa.ai","score":0.3,"text":"more"}
    ]}`
	client := exaMockHTTP(200, respJSON, func(req *http.Request) {
		gotPath = req.URL.String()
		gotKey = req.Header.Get("x-api-key")
		b, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(b, &gotBody)
	})

	backend := NewExaBackend(client, "exa-secret", 5)
	results, err := backend.Search(context.Background(), SearchParams{
		Query:          "search api",
		AllowedDomains: []string{"exa.ai"},
		BlockedDomains: []string{"old.example"},
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if gotPath != exaEndpoint {
		t.Errorf("unexpected URL: %s", gotPath)
	}
	if gotKey != "exa-secret" {
		t.Errorf("expected x-api-key=exa-secret, got %q", gotKey)
	}
	// camelCase field names per Exa API
	if v, _ := gotBody["numResults"].(float64); int(v) != 5 {
		t.Errorf("expected numResults=5, got %v", gotBody["numResults"])
	}
	inc, _ := gotBody["includeDomains"].([]any)
	if len(inc) != 1 || inc[0] != "exa.ai" {
		t.Errorf("expected includeDomains=[exa.ai], got %v", gotBody["includeDomains"])
	}
	exc, _ := gotBody["excludeDomains"].([]any)
	if len(exc) != 1 || exc[0] != "old.example" {
		t.Errorf("expected excludeDomains=[old.example], got %v", gotBody["excludeDomains"])
	}
	contents, _ := gotBody["contents"].(map[string]any)
	if v, _ := contents["text"].(bool); !v {
		t.Errorf("expected contents.text=true, got %v", gotBody["contents"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Exa" || results[0].Snippet != "Exa text" {
		t.Errorf("first result mismatched: %+v", results[0])
	}
}

func TestExaBackend_Search_HTTP401(t *testing.T) {
	client := exaMockHTTP(401, `{"error":"bad key"}`, nil)
	backend := NewExaBackend(client, "bad", 5)
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
	if !strings.Contains(de.Error(), "exa") {
		t.Errorf("error should include backend name, got: %v", de.Error())
	}
}

func TestExaBackend_Search_HTTP5xx(t *testing.T) {
	client := exaMockHTTP(503, `service down`, nil)
	backend := NewExaBackend(client, "k", 5)
	_, err := backend.Search(context.Background(), SearchParams{Query: "x"})
	if err == nil {
		t.Fatal("expected 5xx error")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrServiceUnavailable {
		t.Errorf("expected ErrServiceUnavailable, got %v", de.Code)
	}
}
