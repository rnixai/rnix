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
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- Mock HTTP client ---

type mockRoundTripper struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

func newMockHTTPClient(fn func(req *http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: &mockRoundTripper{fn: fn}}
}

// --- Mock HTML converter ---

type mockConverter struct {
	fn func(html string) (string, error)
}

func (m *mockConverter) Convert(html string) (string, error) {
	if m.fn != nil {
		return m.fn(html)
	}
	return html, nil
}

// --- Mock Search backend ---

type mockSearchBackend struct {
	fn func(ctx context.Context, params SearchParams) ([]SearchResult, error)
}

func (m *mockSearchBackend) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	return m.fn(ctx, params)
}

// --- ToolDef metadata tests (Task 4.1) ---

func TestWebDriver_ToolDefs_Metadata(t *testing.T) {
	d := NewDriver()
	defs := d.ToolDefs()

	if len(defs) != 2 {
		t.Fatalf("expected 2 ToolDefs, got %d", len(defs))
	}

	// Verify web_fetch ToolDef
	fetch := defs[0]
	if fetch.Name != "WebFetch" {
		t.Errorf("expected name 'WebFetch', got %q", fetch.Name)
	}
	if !fetch.IsReadOnly {
		t.Error("web_fetch should be ReadOnly")
	}
	if !fetch.IsConcurrencySafe {
		t.Error("web_fetch should be ConcurrencySafe")
	}
	if !fetch.ShouldDefer {
		t.Error("web_fetch should have ShouldDefer=true")
	}
	if fetch.SearchHint != "web fetch url page content" {
		t.Errorf("unexpected SearchHint: %q", fetch.SearchHint)
	}
	if fetch.MaxResultTokens != 100000 {
		t.Errorf("expected MaxResultTokens=100000, got %d", fetch.MaxResultTokens)
	}
	if fetch.IsDestructive {
		t.Error("web_fetch should NOT be destructive")
	}

	// Verify web_search ToolDef
	search := defs[1]
	if search.Name != "WebSearch" {
		t.Errorf("expected name 'WebSearch', got %q", search.Name)
	}
	if !search.IsReadOnly {
		t.Error("web_search should be ReadOnly")
	}
	if !search.IsConcurrencySafe {
		t.Error("web_search should be ConcurrencySafe")
	}
	if !search.ShouldDefer {
		t.Error("web_search should have ShouldDefer=true")
	}
	if search.SearchHint != "web search query internet" {
		t.Errorf("unexpected SearchHint: %q", search.SearchHint)
	}
	if search.MaxResultTokens != 100000 {
		t.Errorf("expected MaxResultTokens=100000, got %d", search.MaxResultTokens)
	}
}

func TestWebDriver_ToolDescriptorInterface(t *testing.T) {
	var _ vfs.ToolDescriptor = (*WebDriver)(nil)
}

// --- Fetch tests ---

func TestWebFile_Fetch_Success(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("Hello World")),
			Request:    req,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{
		HTTPClient: client,
		Converter:  &mockConverter{},
	})

	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := f.Read(1 << 20)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != "Hello World" {
		t.Errorf("unexpected result: %q", string(data))
	}
}

func TestWebFile_Fetch_HTMLConversion(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<html><body><p>Hello</p></body></html>")),
			Request:    req,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{
		HTTPClient: client,
		Converter:  &mockConverter{fn: func(html string) (string, error) { return "# Converted\nHello", nil }},
	})

	f := &WebFile{driver: d, devicePath: "/dev/web"}
	err := f.Write(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, _ := f.Read(1 << 20)
	if !strings.Contains(string(data), "Converted") {
		t.Errorf("expected converted markdown, got: %q", string(data))
	}
}

func TestWebFile_Fetch_WithPrompt(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("content")),
			Request:    req,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com","prompt":"extract the title"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, _ := f.Read(1 << 20)
	result := string(data)
	if !strings.Contains(result, "extract the title") {
		t.Errorf("expected prompt in result, got: %q", result)
	}
	if !strings.Contains(result, "content") {
		t.Errorf("expected content in result, got: %q", result)
	}
}

// --- ContentExtractor tests ---

type mockExtractor struct {
	fn func(ctx context.Context, content, prompt string) (string, error)
}

func (m *mockExtractor) Extract(ctx context.Context, content, prompt string) (string, error) {
	return m.fn(ctx, content, prompt)
}

func TestWebFile_Fetch_WithExtractor(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("Go is a programming language.")),
			Request:    req,
		}, nil
	})

	extractor := &mockExtractor{fn: func(_ context.Context, content, prompt string) (string, error) {
		return fmt.Sprintf("Extracted: %s from content of length %d", prompt, len(content)), nil
	}}

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}, Extractor: extractor})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com","prompt":"what language is described?"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, _ := f.Read(1 << 20)
	result := string(data)
	if !strings.Contains(result, "Extracted:") {
		t.Errorf("expected extractor output, got: %q", result)
	}
	if strings.Contains(result, "[Extraction prompt]") {
		t.Errorf("should not contain raw prompt tag when extractor succeeds, got: %q", result)
	}
}

func TestWebFile_Fetch_ExtractorFallback(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("raw content")),
			Request:    req,
		}, nil
	})

	extractor := &mockExtractor{fn: func(_ context.Context, _, _ string) (string, error) {
		return "", fmt.Errorf("model unavailable")
	}}

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}, Extractor: extractor})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com","prompt":"extract info"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, _ := f.Read(1 << 20)
	result := string(data)
	// Should fall back to raw content with prompt tag
	if !strings.Contains(result, "[Extraction prompt]") {
		t.Errorf("expected fallback with prompt tag, got: %q", result)
	}
	if !strings.Contains(result, "raw content") {
		t.Errorf("expected raw content in fallback, got: %q", result)
	}
}

func TestWebFile_Fetch_NoPrompt_SkipsExtractor(t *testing.T) {
	extractorCalled := false
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("raw content")),
			Request:    req,
		}, nil
	})

	extractor := &mockExtractor{fn: func(_ context.Context, _, _ string) (string, error) {
		extractorCalled = true
		return "should not be called", nil
	}}

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}, Extractor: extractor})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if extractorCalled {
		t.Error("extractor should not be called when prompt is empty")
	}
}

func TestMakeSecondaryModelPrompt(t *testing.T) {
	result := MakeSecondaryModelPrompt("Hello world", "extract title")
	if !strings.Contains(result, "Hello world") {
		t.Error("expected content in prompt")
	}
	if !strings.Contains(result, "extract title") {
		t.Error("expected prompt text in prompt")
	}
	if !strings.Contains(result, "Web page content:") {
		t.Error("expected prompt structure marker")
	}
	if !strings.Contains(result, "concise response") {
		t.Error("expected guidelines in prompt")
	}
}

func TestWebFile_Fetch_HTTPError(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Status:     "404 Not Found",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com/notfound"}`))
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
	// Story 76.1 AC1: a 404 is informational — the code must be NOT_FOUND, not
	// the DRIVER default bucket that the wicket incident's breaker saw.
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound for 404, got %v", de.Code)
	}
}

func TestWebFile_Fetch_NetworkError(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection refused")
	})

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}

func TestWebFile_Fetch_HTTPUpgradeToHTTPS(t *testing.T) {
	requestURLs := []string{}
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		requestURLs = append(requestURLs, req.URL.String())
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	// Pass HTTP URL — should be upgraded to HTTPS
	err := f.Write(context.Background(), []byte(`{"url":"http://example.com"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if len(requestURLs) == 0 {
		t.Fatal("no requests made")
	}
	if !strings.HasPrefix(requestURLs[0], "https://") {
		t.Errorf("expected HTTPS upgrade, first request was: %s", requestURLs[0])
	}
}

func TestWebFile_Fetch_CrossDomainRedirect(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		// Simulate redirect by changing request URL in response
		redirectReq, _ := http.NewRequest("GET", "https://other-domain.com/page", nil)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("redirected content")),
			Request:    redirectReq,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, _ := f.Read(1 << 20)
	result := string(data)
	if !strings.Contains(result, "Cross-domain redirect") {
		t.Errorf("expected cross-domain redirect notification, got: %q", result)
	}
	if !strings.Contains(result, "other-domain.com") {
		t.Errorf("expected redirect URL in result, got: %q", result)
	}
}

// --- Cache tests ---

func TestWebDriver_Cache_Hit(t *testing.T) {
	callCount := 0
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		callCount++
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("cached content")),
			Request:    req,
		}, nil
	})

	d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})

	// First fetch
	f1 := &WebFile{driver: d, devicePath: "/dev/web"}
	if err := f1.Write(context.Background(), []byte(`{"url":"https://example.com"}`)); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}
	f1.Read(1 << 20)

	// Second fetch — should hit cache
	f2 := &WebFile{driver: d, devicePath: "/dev/web"}
	if err := f2.Write(context.Background(), []byte(`{"url":"https://example.com"}`)); err != nil {
		t.Fatalf("second Write failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 HTTP call (cache hit), got %d", callCount)
	}
}

func TestWebDriver_Cache_Expiry(t *testing.T) {
	d := NewDriver()
	// Manually set an expired cache entry
	d.mu.Lock()
	d.cache["https://example.com"] = cacheEntry{
		data:      "stale",
		expiresAt: time.Now().Add(-1 * time.Minute),
	}
	d.mu.Unlock()

	_, ok := d.getCached("https://example.com")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

// --- Search tests ---

func TestWebFile_Search_Success(t *testing.T) {
	mockSearch := &mockSearchBackend{
		fn: func(ctx context.Context, params SearchParams) ([]SearchResult, error) {
			return []SearchResult{
				{Title: "Result 1", URL: "https://example.com/1", Snippet: "snippet 1"},
				{Title: "Result 2", URL: "https://example.com/2", Snippet: "snippet 2"},
			}, nil
		},
	}

	d := NewDriverWithOptions(DriverOpts{Search: mockSearch})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"query":"test search"}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, _ := f.Read(1 << 20)
	var results []SearchResult
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("failed to parse results: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestWebFile_Search_WithDomainFilters(t *testing.T) {
	var capturedAllowed, capturedBlocked []string
	var capturedMax int
	mockSearch := &mockSearchBackend{
		fn: func(ctx context.Context, params SearchParams) ([]SearchResult, error) {
			capturedAllowed = params.AllowedDomains
			capturedBlocked = params.BlockedDomains
			capturedMax = params.MaxResults
			return []SearchResult{}, nil
		},
	}

	d := NewDriverWithOptions(DriverOpts{Search: mockSearch})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"query":"test","allowed_domains":["docs.go.dev"],"blocked_domains":["spam.com"],"max_results":7}`))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if len(capturedAllowed) != 1 || capturedAllowed[0] != "docs.go.dev" {
		t.Errorf("unexpected allowed_domains: %v", capturedAllowed)
	}
	if len(capturedBlocked) != 1 || capturedBlocked[0] != "spam.com" {
		t.Errorf("unexpected blocked_domains: %v", capturedBlocked)
	}
	if capturedMax != 7 {
		t.Errorf("expected max_results=7, got %d", capturedMax)
	}
}

func TestWebFile_Search_BackendError(t *testing.T) {
	mockSearch := &mockSearchBackend{
		fn: func(ctx context.Context, params SearchParams) ([]SearchResult, error) {
			return nil, fmt.Errorf("search backend unavailable")
		},
	}

	d := NewDriverWithOptions(DriverOpts{Search: mockSearch})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error for search backend failure")
	}
}

// --- VFSFile lifecycle tests ---

func TestWebFile_Lifecycle(t *testing.T) {
	d := NewDriver()
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	// Read before Write
	_, err := f.Read(100)
	if err == nil {
		t.Error("expected error reading before write")
	}

	// Close
	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close
	if err := f.Close(); err == nil {
		t.Error("expected error on double close")
	}

	// Write after close
	if err := f.Write(context.Background(), []byte(`{"url":"https://example.com"}`)); err == nil {
		t.Error("expected error writing to closed file")
	}

	// Read after close
	if _, err := f.Read(100); err == nil {
		t.Error("expected error reading from closed file")
	}
}

func TestWebFile_Stat(t *testing.T) {
	d := NewDriver()
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/web" {
		t.Errorf("expected DevicePath='/dev/web', got %q", stat.DevicePath)
	}
}

func TestWebFile_InvalidJSON(t *testing.T) {
	d := NewDriver()
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWebFile_MissingURLAndQuery(t *testing.T) {
	d := NewDriver()
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{}`))
	if err == nil {
		t.Error("expected error when neither url nor query provided")
	}
}

func TestWebFile_InvalidURL(t *testing.T) {
	d := NewDriverWithOptions(DriverOpts{Converter: &mockConverter{}})
	f := &WebFile{driver: d, devicePath: "/dev/web"}

	err := f.Write(context.Background(), []byte(`{"url":"not-a-url"}`))
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

// --- FileFactory test ---

func TestFileFactory(t *testing.T) {
	d := NewDriver()
	factory := FileFactory(d)

	file, err := factory("", vfs.O_RDWR, "/tmp")
	if err != nil {
		t.Fatalf("FileFactory failed: %v", err)
	}

	wf, ok := file.(*WebFile)
	if !ok {
		t.Fatal("expected *WebFile")
	}
	if wf.devicePath != "/dev/web" {
		t.Errorf("unexpected devicePath: %q", wf.devicePath)
	}
}

// --- Helper tests ---

func TestIsCrossDomainRedirect(t *testing.T) {
	tests := []struct {
		original string
		final    string
		want     bool
	}{
		{"https://a.com/p", "https://a.com/q", false},
		{"https://a.com/p", "https://b.com/q", true},
		{"invalid", "https://b.com", false},
	}
	for _, tt := range tests {
		got := isCrossDomainRedirect(tt.original, tt.final)
		if got != tt.want {
			t.Errorf("isCrossDomainRedirect(%q, %q) = %v, want %v", tt.original, tt.final, got, tt.want)
		}
	}
}

func TestLooksLikeHTML(t *testing.T) {
	if !looksLikeHTML("<!DOCTYPE html><html>") {
		t.Error("expected true for DOCTYPE html")
	}
	if !looksLikeHTML("<html><body>") {
		t.Error("expected true for <html>")
	}
	if looksLikeHTML("plain text content") {
		t.Error("expected false for plain text")
	}
}

func TestSimpleHTMLConverter(t *testing.T) {
	c := &simpleHTMLConverter{}
	result, err := c.Convert("<html><body><p>Hello</p><script>alert('x')</script></body></html>")
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}
	if strings.Contains(result, "alert") {
		t.Error("script content should be removed")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("expected 'Hello' in output")
	}
}

// --- SearXNG backend domain filter test ---

func TestSearxngBackend_BlockedDomains(t *testing.T) {
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		respJSON := `{"results":[
			{"title":"Good","url":"https://good.com/page","content":"ok"},
			{"title":"Bad","url":"https://spam.com/page","content":"spam"}
		]}`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(respJSON)),
		}, nil
	})

	backend := &searxngBackend{client: client, baseURL: "http://localhost:8888"}
	results, err := backend.Search(context.Background(), SearchParams{Query: "test", BlockedDomains: []string{"spam.com"}})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (blocked spam.com), got %d", len(results))
	}
	if results[0].Title != "Good" {
		t.Errorf("expected 'Good' result, got %q", results[0].Title)
	}
}

func TestSearxngBackend_NoBaseURL(t *testing.T) {
	backend := &searxngBackend{client: nil, baseURL: ""}
	_, err := backend.Search(context.Background(), SearchParams{Query: "test"})
	if err == nil {
		t.Error("expected error when baseURL is empty")
	}
}

// --- Device registration integration test (Task 4.3) ---

func TestDeviceRegistration_Integration(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()

	webDriver := NewDriverWithOptions(DriverOpts{
		Search: &mockSearchBackend{fn: func(ctx context.Context, params SearchParams) ([]SearchResult, error) {
			return nil, nil
		}},
	})
	err := devReg.RegisterWithDriver("/dev/web", FileFactory(webDriver), webDriver)
	if err != nil {
		t.Fatalf("RegisterWithDriver failed: %v", err)
	}

	driver, ok := devReg.GetDriver("/dev/web")
	if !ok {
		t.Fatal("driver not found after registration")
	}

	td, ok := driver.(vfs.ToolDescriptor)
	if !ok {
		t.Fatal("driver does not implement ToolDescriptor")
	}

	defs := td.ToolDefs()
	if len(defs) != 2 {
		t.Errorf("expected 2 ToolDefs, got %d", len(defs))
	}
	if defs[0].Name != "WebFetch" {
		t.Errorf("expected first ToolDef name 'WebFetch', got %q", defs[0].Name)
	}
	if defs[1].Name != "WebSearch" {
		t.Errorf("expected second ToolDef name 'WebSearch', got %q", defs[1].Name)
	}

	found := false
	devReg.RangeDrivers(func(path string, d any) bool {
		if path == "/dev/web" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("driver not found via RangeDrivers")
	}
}
