# Changelog

All notable changes are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- Updated GitHub Actions to Node.js 24-compatible major versions.
- Run race tests on stable Go while retaining Go 1.22 compatibility tests on Linux; current macOS runners cannot execute Go 1.22 Mach-O test binaries.

## [0.1.0] - 2026-08-23

### Added

- Go CLI for NVIDIA CUDA documentation search.
- Browser OAuth login with PKCE, token refresh, and logout.
- Tool discovery, generic tool calls, human output, and JSON output.
- Unit and local HTTP integration tests.
- CI, release automation, security policy, and contributor documentation.
- Fully static Linux and cgo-free single-file macOS release builds for amd64 and arm64.
