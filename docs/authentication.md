# Authentication

NVIDIA's CUDA documentation endpoint responds with `401 Unauthorized` until the caller completes OAuth. `cuda-docs login` implements Authorization Code with PKCE:

1. Register a local CLI client with NVIDIA's dynamic registration endpoint.
2. Generate a random verifier, SHA-256 challenge, and callback state.
3. Listen on `127.0.0.1:8765` (or the selected port).
4. Open the NVIDIA authorization page.
5. Validate the loopback callback state and exchange the code.
6. Save the access and refresh tokens in the OS user config directory.

The CLI refreshes an access token shortly before expiry. A revoked or otherwise rejected token requires another `cuda-docs login`.

## Headless machines

`--no-browser` prints the URL, but the browser must be able to reach the callback address on the same machine. For a remote SSH host, forward the callback port before starting login:

```bash
ssh -L 8765:127.0.0.1:8765 gpu-host
cuda-docs login --no-browser
```

Open the printed URL locally. Keep the terminal running until the callback completes.

## Removing credentials

```bash
cuda-docs logout
```

This removes the local credential file. It does not revoke an NVIDIA account session globally.
