# Changelog

All notable changes to Agent Observer will be documented here.

## Unreleased

- Added production CLI metadata, diagnostics, doctor checks, logging, redaction, and release automation.
- Hardened scanner behavior around oversized files, symlinks, deleted files, and mid-write reads.
- Added CI verification for test, race, vet, and build on Linux and macOS.
- Changed the local shell pane to disabled by default with explicit `--shell` opt-in.
- Added opt-in aggregate telemetry configuration and safe event payloads.
- Added compact TUI layout improvements, NO_COLOR support, shell completions, man page, and macOS signing guidance.
