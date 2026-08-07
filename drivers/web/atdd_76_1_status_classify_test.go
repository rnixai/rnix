package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// Story 76.1 — AC1/AC2: upstream HTTP status is classified into typed
// DriverError codes so the kernel's informational-error exemption keeps agents
// alive while they respond to 404s (wicket incident: 3 real upstream 404s
// surfaced as DRIVER|/dev/web and killed the step).
//
// The Device field is asserted alongside the Code to catch a fetch branch that
// classifies correctly but names the wrong device. Note the breaker's
// fingerprint is errCode|toolPath where toolPath comes from the kernel-side
// mapping.VFSPath (kernel/tool_exec.go), NOT from DriverError.Device — the two
// happen to agree today because /dev/web is the registration path.

func TestATDD_76_1_AC1_FetchClassifiesStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   types.ErrCode
	}{
		{"404 not found", 404, types.ErrNotFound},
		{"410 gone shares the 404 class", 410, types.ErrNotFound},
		{"401 unauthorized", 401, types.ErrPermission},
		{"402 payment required", 402, types.ErrPermission},
		{"403 forbidden", 403, types.ErrPermission},
		{"429 rate limited", 429, types.ErrServiceUnavailable},
		{"500 upstream down", 500, types.ErrServiceUnavailable},
		{"502 bad gateway", 502, types.ErrServiceUnavailable},
		{"599 upper bound of 5xx", 599, types.ErrServiceUnavailable},
		{"400 default bucket", 400, types.ErrDriver},
		{"405 default bucket", 405, types.ErrDriver},
		{"408 request timeout is upstream-side, not our deadline", 408, types.ErrDriver},
		// >=600 is not HTTP. LinkedIn answers bots with 999; classifying it as
		// ServiceUnavailable would feed the epic-73 backoff chain into retrying
		// a permanent ban.
		{"999 linkedin bot ban is not transient", 999, types.ErrDriver},
		{"600 just past the 5xx band", 600, types.ErrDriver},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.status,
					Status:     fmt.Sprintf("%d %s", tc.status, http.StatusText(tc.status)),
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("upstream body")),
					Request:    req,
				}, nil
			})
			d := NewDriverWithOptions(DriverOpts{HTTPClient: client, Converter: &mockConverter{}})
			f := &WebFile{driver: d, devicePath: "/dev/web"}

			err := f.Write(context.Background(), []byte(`{"url":"https://example.com/missing"}`))
			if err == nil {
				t.Fatal("expected error for non-2xx status")
			}
			var de *types.DriverError
			if !errors.As(err, &de) {
				t.Fatalf("expected *types.DriverError, got %T", err)
			}
			if de.Code != tc.want {
				t.Errorf("status %d: Code = %v, want %v", tc.status, de.Code, tc.want)
			}
			if de.Device != "/dev/web" {
				t.Errorf("status %d: Device = %q, want /dev/web (fingerprint is errCode|toolPath)", tc.status, de.Device)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%d", tc.status)) {
				t.Errorf("status %d: error message must keep the status code, got: %v", tc.status, err)
			}
		})
	}
}

// AC2: the search backends share the classifier table, so a 404 now carries
// NOT_FOUND (previously the default bucket → DRIVER).
func TestATDD_76_1_AC2_TavilySearch404IsNotFound(t *testing.T) {
	client := tavilyMockHTTP(404, `{"error":"not found"}`, nil)
	backend := NewTavilyBackend(client, "k", 5, "")
	_, err := backend.Search(context.Background(), SearchParams{Query: "x"})
	if err == nil {
		t.Fatal("expected error for 404")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", de.Code)
	}
	if !strings.Contains(de.Error(), "tavily") {
		t.Errorf("error should include backend name, got: %v", de.Error())
	}
}
