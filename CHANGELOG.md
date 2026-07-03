# Changelog

All notable changes to Agent Observer will be documented here.

## Unreleased

- Schema v2: snapshots now carry `sessions` and `providers` alongside `tasks` and `batches`. Existing `tasks`/`batches` fields are unchanged.
- Multi-provider observation: built-in adapters for Claude Code (live sessions + transcripts with model/turns/tokens/last message), Codex CLI (rollout events + session index titles), and Cursor (transcript recency), plus declarative TOML/JSON provider manifests in `~/.config/agent-observer/providers` — no recompile needed for new tools.
- Responsiveness: fsnotify-driven updates (directory watches + bounded hot-file watching, 75ms debounce) with a 5s polling safety net (`--poll-interval`, `--no-watch`); file change to rendered snapshot in well under 100ms. Incremental JSONL tailing keeps scans cheap on large transcripts.
- Agent actions (opt-in `--act`): launch (`n`), resume (`R`), steer, and stop (`x`, SIGTERM then SIGKILL with confirmation) agent runs in up to 4 managed PTY panes (`[`/`]` cycle). Exact argv is always displayed; commands run via the user's installed CLIs and subscriptions, never through a shell.
- New flags/env: `--providers`, `--claude-dir`, `--codex-dir`, `--cursor-dir`, `--plugins-dir`, `--poll-interval`, `--no-watch`, `--act`/`--no-act`, and an optional `~/.config/agent-observer/config.toml` (flags > env > file).
- Doctor and diagnostics now report per-provider detection, session counts, and warn when actions are enabled; action telemetry events (`action.launch/resume/stop`) respect the existing opt-in gate.
- Added production CLI metadata, diagnostics, doctor checks, logging, redaction, and release automation.
- Hardened scanner behavior around oversized files, symlinks, deleted files, and mid-write reads.
- Added CI verification for test, race, vet, and build on Linux and macOS.
- Changed the local shell pane to disabled by default with explicit `--shell` opt-in.
- Added opt-in aggregate telemetry configuration and safe event payloads.
- Added compact TUI layout improvements, NO_COLOR support, shell completions, man page, and macOS signing guidance.
