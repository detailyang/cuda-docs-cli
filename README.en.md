# cuda-docs-cli

English | [简体中文](README.md)

`cuda-docs-cli` turns NVIDIA's CUDA documentation service into a normal command-line tool. You do not need to install, configure, or run an MCP client.

```console
$ cuda-docs search "how to reduce shared memory bank conflicts"
...
```

> [!IMPORTANT]
> NVIDIA currently exposes this service only through a remote MCP HTTP endpoint and requires OAuth. This single Go binary handles OAuth, JSON-RPC, and session details internally; it does not expose an MCP configuration or run a background service.

This project is not affiliated with or endorsed by NVIDIA. CUDA, Nsight, and NVIDIA are trademarks of NVIDIA Corporation.

## Features

- `search` discovers and invokes the server's documentation search capability.
- `tools` shows the currently advertised tools and input schemas.
- `call` provides a generic escape hatch for new server capabilities.
- Browser-based OAuth login, automatic token refresh, and credential removal.
- Human-readable output or structured JSON for scripts.
- One Go binary with no Python, Node.js, or MCP client dependency.

This tool queries documentation only. It does not collect or analyze GPU performance data.

## Install

Build with Go 1.22 or newer:

```bash
git clone https://github.com/detailyang/cuda-docs-cli.git
cd cuda-docs-cli
make build
install -m 0755 bin/cuda-docs ~/.local/bin/cuda-docs
```

Once the repository is published, Go users can install it directly:

```bash
go install github.com/detailyang/cuda-docs-cli/cmd/cuda-docs@latest
```

Release archives contain Linux and macOS binaries for amd64 and arm64. Release builds use `CGO_ENABLED=0`, `netgo`, and `osusergo`: Linux artifacts are fully statically linked, while macOS artifacts are cgo-free single binaries linked only to frameworks shipped with macOS. Neither requires a Go runtime or separately installed shared libraries.

## Quick start

```bash
cuda-docs login
cuda-docs search "CUDA graph launch overhead"
cuda-docs search --json "coalesced global memory access" | jq .
```

For SSH or headless environments, print the URL instead of launching a browser. Change the callback port if 8765 is occupied:

```bash
cuda-docs login --no-browser --port 9876
```

## Commands

```text
cuda-docs login [--port 8765] [--no-browser]
cuda-docs logout
cuda-docs search [--json] <query>
cuda-docs tools [--json]
cuda-docs call [--args JSON] [--json] <tool-name>
cuda-docs version
```

Inspect raw server capabilities or invoke one explicitly:

```bash
cuda-docs tools --json
cuda-docs call --args '{"query":"CUDA streams"}' TOOL_NAME
```

Go's standard `flag` syntax requires options to appear before the query or tool name.

## Configuration and privacy

Credentials are stored in the operating system's user config directory under `cuda-docs-cli/credentials.json`. The file is atomically replaced and has mode `0600` on Unix. Queries are sent to NVIDIA's documentation service. The CLI has no telemetry, database, or third-party proxy.

| Environment variable | Purpose |
| --- | --- |
| `CUDA_DOCS_CONFIG_DIR` | Override the credential directory for CI or isolated testing |
| `CUDA_DOCS_ENDPOINT` | Override the documentation endpoint for development |

See [architecture](docs/architecture.md), [authentication](docs/authentication.md), [troubleshooting](docs/troubleshooting.md), [contributing](CONTRIBUTING.md), and [security](SECURITY.md) for details.

## License

[MIT](LICENSE)
