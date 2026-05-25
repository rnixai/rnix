# /dev/web Search Backends

The `/dev/web` device exposes two tools:

- `WebFetch(url, prompt?)` — fetch a single URL, return Markdown.
- `WebSearch(query, allowed_domains?, blocked_domains?, max_results?)` — query a search backend.

`WebSearch` dispatches to one of three pluggable backends. This guide covers configuration.

## Quick start (one env var)

| Backend  | Env var           | Notes                                |
|----------|-------------------|--------------------------------------|
| Tavily   | `TAVILY_API_KEY`  | Free tier at <https://tavily.com>    |
| Exa      | `EXA_API_KEY`     | Paid; research-grade quality         |
| SearXNG  | `RNIX_SEARCH_URL` | Self-hosted, e.g. `http://localhost:8888` |

Set exactly one and the daemon auto-detects it on startup. Priority when several are set:
`TAVILY_API_KEY > EXA_API_KEY > RNIX_SEARCH_URL`. Look for a daemon log line like
`[web] search backend auto-detected: tavily` to confirm.

## Explicit configuration: `web-search.yaml`

For multi-backend setups, drop a YAML file at one of:

1. `<project>/.rnix/web-search.yaml` (project, takes priority)
2. `~/.config/rnix/web-search.yaml` (global)

```yaml
version: "1"
default_backend: tavily
backends:
  - name: tavily
    driver: tavily
    api_key_env: TAVILY_API_KEY   # resolved from project .env first, then os env
    max_results: 5
    search_depth: basic            # basic | advanced
  - name: exa
    driver: exa
    api_key_env: EXA_API_KEY
    num_results: 5
  - name: local-searxng
    driver: searxng
    base_url: http://localhost:8888
```

The project file fully overrides the global file (no merging) so each project can pick its own
defaults. API keys are looked up via `.env` (`.env.local`, `.env.<RNIX_ENV>`, etc.) before
falling back to the host environment — keep keys out of the YAML itself.

## Domain filters

`WebSearch` accepts `allowed_domains` and `blocked_domains` arrays. They map to:

- Tavily: `include_domains` / `exclude_domains` (server-side)
- Exa: `includeDomains` / `excludeDomains` (server-side, note camelCase)
- SearXNG: `site:` operators in the query + client-side filter for `blocked`

## Common errors

| Symptom                                                 | Fix                                                         |
|---------------------------------------------------------|-------------------------------------------------------------|
| `WebSearch backend not configured...`                   | Set one of the three env vars, or write `web-search.yaml`.  |
| `tavily: HTTP 401` / `exa: HTTP 401`                    | API key invalid or revoked — verify the env var value.      |
| `tavily: HTTP 429`                                      | Free-tier rate limit. Wait or upgrade your plan.            |
| `[web] backend X skipped: missing API key`              | YAML lists backend X but its `api_key_env` resolves empty.  |

Errors surface to the agent as a `DriverError` so the LLM can choose to retry, switch
strategy, or surface a config hint to the user — there is no silent failure path.
