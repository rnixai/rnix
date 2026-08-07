package web

import (
	"fmt"
	"net/http"

	"github.com/rnixai/rnix/internal/types"
)

// classifyStatus maps an upstream HTTP status code to the error class the
// circuit breaker treats it as (story 76.1, AC1 + AC2):
//   - 404/410     → ErrNotFound (upstream resource missing — informational: the
//     agent should try another URL/path, and the breaker must not count it).
//     410 Gone states the same thing more strongly (permanently removed rather
//     than merely absent) and calls for the identical agent response, so it
//     shares the class instead of falling through to the default bucket.
//   - 401/402/403 → ErrPermission (credentials invalid / payment required /
//     forbidden — all three mean "this request is not allowed as posed")
//   - 429         → ErrServiceUnavailable (rate limited)
//   - 5xx         → ErrServiceUnavailable (upstream down). Bounded at 599 on
//     purpose: >=600 is not HTTP at all (LinkedIn answers bots with 999,
//     proxies inject private codes), and those are permanent refusals —
//     marking them transient would feed the epic-73 429 backoff chain into
//     retrying something that will never recover.
//   - other non-2xx → ErrDriver (unexpected upstream behaviour)
//
// This single table is shared by the fetch path (driver.go) and all three
// search backends (exa/tavily/searxng via classifyHTTPError), so a status is
// classified identically regardless of which WebFetch/WebSearch route hit it.
func classifyStatus(status int) types.ErrCode {
	switch {
	case status == http.StatusNotFound || status == http.StatusGone:
		return types.ErrNotFound
	case status == http.StatusUnauthorized ||
		status == http.StatusPaymentRequired ||
		status == http.StatusForbidden:
		return types.ErrPermission
	case status == http.StatusTooManyRequests:
		return types.ErrServiceUnavailable
	case status >= 500 && status <= 599:
		return types.ErrServiceUnavailable
	default:
		return types.ErrDriver
	}
}

// classifyHTTPError maps an upstream HTTP status code to a typed DriverError
// for the search backends (Op "Search", Device "/dev/web").
//
// body is included in the error message (truncated) to aid debugging when the
// upstream returns a structured error payload.
func classifyHTTPError(backend string, status int, body []byte) *types.DriverError {
	bodySnippet := truncateBody(body, 256)
	return &types.DriverError{
		Op:     "Search",
		Device: "/dev/web",
		Err:    fmt.Errorf("%s: HTTP %d: %s", backend, status, bodySnippet),
		Code:   classifyStatus(status),
	}
}

func truncateBody(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}
