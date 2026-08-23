# Contributing

Thank you for improving `cuda-docs-cli`. Small, focused changes are easiest to review.

## Before opening a change

1. Search existing issues and discussions.
2. Open an issue before large behavior, protocol, or command-line changes.
3. Do not include OAuth tokens, client secrets, CUDA source code you cannot share, or captured query data.

## Development workflow

Requirements: Go 1.22+, GNU Make, and Git.

```bash
git clone https://github.com/detailyang/cuda-docs-cli.git
cd cuda-docs-cli
make all
```

Behavior changes should start with a failing test at the public boundary. Network tests use `httptest`; normal tests must not contact NVIDIA or open a browser.

Before submitting:

```bash
make fmt-check
make vet
make test
make build
```

Update the relevant README or document when command behavior, authentication, configuration, privacy, or protocol assumptions change. Add an entry under `Unreleased` in `CHANGELOG.md` for user-visible changes.

## Pull requests

- Explain the user problem and the smallest chosen solution.
- Include test evidence and any manual verification.
- Keep generated binaries and credentials out of the repository.
- Use clear commits. Maintainers may squash changes when merging.

By contributing, you agree that your contribution is licensed under the MIT License and to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
