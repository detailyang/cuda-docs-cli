# Security policy

## Supported versions

Security fixes are provided for the latest released minor version. Before the first stable release, fixes are made on `main` and included in the next release.

## Reporting a vulnerability

Do not open a public issue for vulnerabilities involving credential disclosure, OAuth state validation, callback handling, request forgery, or unsafe file permissions. Use GitHub's private vulnerability reporting for this repository. If that feature is unavailable, contact the repository owner through the private contact method listed on their GitHub profile.

Include affected versions, reproduction steps, impact, and a proposed mitigation if known. Never include a working NVIDIA access token or client secret. You should receive an acknowledgement within seven days.

## Security model

- OAuth uses Authorization Code with PKCE and verifies the callback `state` value.
- The callback server binds only to `127.0.0.1` and stops after login or timeout.
- Credentials are atomically stored in the OS user config directory and set to `0600` on Unix.
- Access tokens are sent only to the configured documentation endpoint.
- `CUDA_DOCS_ENDPOINT` intentionally changes the trust boundary; do not set it to an untrusted server.
- The CLI has no telemetry and does not execute content returned by the documentation service.

See [docs/architecture.md](docs/architecture.md) for the full boundary description.
