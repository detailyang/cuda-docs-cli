# Troubleshooting

## `not logged in; run: cuda-docs login`

Complete browser login first. If you copied the credential file between machines, delete it with `cuda-docs logout` and log in again.

## Callback port is already in use

Choose another loopback port and use the same command throughout that login attempt:

```bash
cuda-docs login --port 9876
```

## Browser opens but the terminal times out

The callback must reach the machine running the CLI. On an SSH host, use local port forwarding as described in [authentication.md](authentication.md). Corporate proxies or browser extensions may also block the redirect.

## `NVIDIA login is invalid or expired`

Run `cuda-docs login` again. The server may reject a token before its advertised expiry if it was revoked.

## No compatible search tool

The service's advertised schema may have changed. Inspect it:

```bash
cuda-docs tools --json
```

Then call the relevant tool explicitly:

```bash
cuda-docs call --args '{"FIELD":"QUERY"}' TOOL_NAME
```

Please open an issue with the redacted tool name and input schema. Do not attach tokens or client secrets.

## JSON is needed for automation

Place `--json` before the query or tool name:

```bash
cuda-docs search --json "CUDA streams"
cuda-docs call --json --args '{"query":"CUDA streams"}' TOOL_NAME
```

## Debugging against a test endpoint

`CUDA_DOCS_ENDPOINT` changes where credentials and queries are sent. It exists for local development and must never point to an untrusted service.
