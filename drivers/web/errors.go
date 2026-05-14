package web

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
)

// ErrSearchNotConfigured returns the agent-friendly DriverError used when a
// caller invokes web_search but no backend has been registered. The message
// lists all three supported configuration paths so the agent can guide the
// user instead of claiming the tool is unimplemented.
func ErrSearchNotConfigured() *types.DriverError {
	return &types.DriverError{
		Op:     "Search",
		Device: "/dev/web",
		Err: fmt.Errorf("web_search backend not configured" +
			"\nSet one of:" +
			"\n  - TAVILY_API_KEY (free tier at https://tavily.com)" +
			"\n  - EXA_API_KEY (https://exa.ai)" +
			"\n  - RNIX_SEARCH_URL (self-hosted SearXNG)" +
			"\nor create ~/.config/rnix/web-search.yaml"),
		Code: types.ErrServiceUnavailable,
	}
}

// wrapBackendError formats a backend-level error consistently, prefixing the
// backend name into the message so /dev/web errors are easy to triage in logs.
func wrapBackendError(backend, op string, err error, code types.ErrCode) *types.DriverError {
	return &types.DriverError{
		Op:     op,
		Device: "/dev/web",
		Err:    fmt.Errorf("%s: %w", backend, err),
		Code:   code,
	}
}
