# Architecture

## Goal and boundary

The user interacts with one short-lived CLI process. There is no MCP configuration, plugin, sidecar, daemon, or local protocol server.

```text
terminal
  └─ cuda-docs
       ├─ OAuth manager ── browser + loopback callback
       ├─ NVIDIA client ── authenticated HTTPS requests
       └─ renderer ─────── text or JSON
                              │
                              ▼
                NVIDIA CUDA documentation service
```

NVIDIA exposes the documentation service at a remote MCP HTTP endpoint. The `internal/nvidia` package contains the minimum client-side wire implementation required to use that endpoint: initialization, session headers, JSON-RPC envelopes, SSE/JSON responses, tool pagination, and tool calls. This protocol is an implementation detail, not a user-facing integration.

## Packages

| Package | Responsibility |
| --- | --- |
| `cmd/cuda-docs` | Process lifecycle, signals, version injection |
| `internal/cli` | Command parsing, exit codes, text/JSON rendering |
| `internal/oauth` | Dynamic client registration, PKCE, callback, refresh, credential storage |
| `internal/nvidia` | Authenticated documentation-session transport and tool schema mapping |

The code uses only the Go standard library. Public behavior is tested through the CLI application boundary; external HTTP behavior is tested with `httptest` servers.

## Query flow

1. Load or refresh the OAuth access token.
2. Initialize a short-lived remote documentation session.
3. Fetch all advertised tool pages.
4. Select a search-like tool with one compatible string input.
5. Call the tool and render text content or raw JSON.

If server schemas change incompatibly, `tools --json` and `call` provide an inspectable escape hatch instead of guessing arguments.

## Security decisions

- PKCE protects the authorization code; random `state` protects the callback against CSRF.
- The callback binds to loopback only and has a five-minute timeout.
- Credentials are never printed and are stored with restricted permissions.
- Response bodies are size-limited before parsing.
- Tool results are printed as data and never evaluated or passed to a shell.
