package web

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
)

// classifyHTTPError maps an upstream HTTP status code to a typed DriverError.
//
// Mapping (per AC#8 of story 41.1):
//   - 401/403 → ErrPermission (API key invalid / forbidden)
//   - 429     → ErrServiceUnavailable (rate limited)
//   - 5xx     → ErrServiceUnavailable (upstream down)
//   - other non-2xx → ErrDriver (unexpected upstream behaviour)
//
// body is included in the error message (truncated) to aid debugging when the
// upstream returns a structured error payload.
func classifyHTTPError(backend string, status int, body []byte) *types.DriverError {
	bodySnippet := truncateBody(body, 256)
	var code types.ErrCode
	switch {
	case status == 401 || status == 403:
		code = types.ErrPermission
	case status == 429:
		code = types.ErrServiceUnavailable
	case status >= 500:
		code = types.ErrServiceUnavailable
	default:
		code = types.ErrDriver
	}
	return &types.DriverError{
		Op:     "Search",
		Device: "/dev/web",
		Err:    fmt.Errorf("%s: HTTP %d: %s", backend, status, bodySnippet),
		Code:   code,
	}
}

func truncateBody(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
