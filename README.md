# Agent Observer

Terminal-first, multi-provider dashboard for local coding agents. Agent Observer watches the on-disk state that Claude Code, Codex CLI, and Cursor leave behind, derives live session and task status, and renders an operations board that reacts to file changes in under 100ms (fsnotify with a polling safety net).

Observation never calls model APIs and is read-only toward provider state. With the opt-in `--act` flag, the dashboard can also launch, steer, stop, and resume agent runs by spawning your installed provider CLIs in managed PTY panes — powered by whatever subscriptions those CLIs already hold. No API keys.

## Providers And Data Sources

Built-in providers (auto-detected; select with `--providers`):

- `claude`: `~/.claude/tasks`, `~/.claude/teams` (optional), `~/.claude/sessions` (live busy/idle status per process), `~/.claude/projects` (session transcripts: model, turns, last message, token usage)
- `codex`: `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` (events, model, tokens), `~/.codex/session_index.jsonl` (thread titles)
- `cursor`: `~/.cursor/projects/*/agent-transcripts` (recency-based status)

Anything else plugs in declaratively — drop a manifest into `~/.config/agent-observer/providers/`:

```toml
name = "aider"

[watch]
globs = ["~/.aider/history/*.jsonl"]

[session]
id = "filename_stem"                 # or dir_name, or field:<dotted.path>
cwd = "field:cwd"
text = "field:message.content.text"
status = { mode = "recency", busy_within = "30s", idle_within = "10m" }

[commands]                            # used only with --act; argv arrays, never shell strings
launch = ["aider", "--message", "{prompt}"]
resume = ["aider", "--resume", "{session_id}"]
```

Large transcripts are tailed incrementally (byte offsets, rotation-safe), so scans stay cheap no matter how much history exists.

## Quickstart

```bash
make run
```

Run the current snapshot binary directly:

```bash
./dist/agent-observer_darwin_arm64_v8.0/agent-observer --version
./dist/agent-observer_darwin_arm64_v8.0/agent-observer doctor --redact
./dist/agent-observer_darwin_arm64_v8.0/agent-observer watch
```

Smoke-test the binary:

```bash
make smoke
```

Install the matching snapshot binary locally:

```bash
make install-snapshot
```

Direct command:

```bash
go run ./cmd/agent-observer --debug
```

Print release metadata:

```bash
agent-observer --version
```

Recommended workflow before starting a Claude subagent/team task:

```bash
cd ~/code/my-project
agent-observer watch --refresh-interval 1s
```

Then start your Claude Code task with subagents. Agent Observer will show the current active run first and keep inactive history collapsed.

Run from any project:

```bash
"/Users/dsouzas/Documents/agent-observer-dashboard/scripts/agent-observer" --debug
```

## Keybindings

- `tab` / `shift+tab`: move dashboard focus
- `1` / `2` / `3` / `4`: focus runs, tasks, details, or health/shell
- `j` / `k` or arrow keys: move selection
- `pgup` / `pgdown` / `home` / `end`: page or jump within the focused panel
- `enter`: toggle between the task list and selected-task details
- `/`: filter runs, tasks, and warnings; `esc` clears the filter
- `i`: toggle inactive batches
- `w`: focus warnings/health when the shell is not open
- `s`: open/focus `shell://local` when started with `--shell`
- `ctrl+o`: detach keyboard focus from shell back to dashboard
- `ctrl+d`: exit shell when shell is focused
- `r`: refresh (pokes the scan engine; scans normally arrive via file events)
- `?`: help
- `q`: quit from dashboard focus

Agent actions, only with `--act`:

- `n`: launch a new agent run (provider picker, prompt, working directory)
- `R`: resume the selected observed session in its own cwd
- `x`: stop the active managed run (confirmation, then SIGTERM → SIGKILL)
- `[` / `]`: cycle between managed PTY panes

Mouse support:

- Click a panel to focus it.
- Click a run, task, or warning row to select it.
- Scroll the hovered panel with the mouse wheel.
- Click footer actions such as inactive, refresh, help, filter, and shell.

## User Workflow

1. Open the project you want Claude Code to work on.
2. Start Agent Observer with `agent-observer watch`.
3. In Claude Code, ask for a task that uses subagents or an agent team.
4. Watch the `Current Run` pane update as local Claude task files appear.
5. Use the Runs, Tasks, Details, and Health panels to inspect current work, open task details, filter noisy runs, and triage warnings.
6. Use mouse clicks/wheel for direct navigation, or `s` for a local project shell when started with `--shell`.

The UI intentionally says `Current Run` and `Recent Run` instead of exposing Claude batch IDs as the primary mental model.

## Color Coding

- Green: running work
- Amber: waiting/pending work
- Red: blocked or errored work
- Gray: completed/inactive work

## Shell Pane

The shell pane is a real local PTY. It is explicitly labeled `shell://local`, starts in the directory where Agent Observer was launched, and is not Claude control.

The pane includes a small terminal screen buffer for common ANSI/CSI behavior: cursor movement, clear screen/line, alternate-screen reset, tabs, backspace, and resize. It also keeps a sanitized transcript fallback so normal command output stays visible while terminal emulation matures.

The shell pane is disabled by default in production builds. Enable it explicitly:

```bash
agent-observer watch --shell
```

## Configuration

CLI flags override environment variables. Useful environment variables:

- `AGENT_OBSERVER_PROVIDERS` (comma-separated: claude, codex, cursor, plugins)
- `AGENT_OBSERVER_CLAUDE_DIR` / `AGENT_OBSERVER_CODEX_DIR` / `AGENT_OBSERVER_CURSOR_DIR`
- `AGENT_OBSERVER_PLUGINS_DIR`
- `AGENT_OBSERVER_TASKS_DIR`
- `AGENT_OBSERVER_TEAMS_DIR`
- `AGENT_OBSERVER_MAX_FILE_SIZE`
- `AGENT_OBSERVER_POLL_INTERVAL` (safety-net rescan behind file watching, default 5s)
- `AGENT_OBSERVER_NO_WATCH` (disable fsnotify; poll only)
- `AGENT_OBSERVER_REFRESH_INTERVAL`
- `AGENT_OBSERVER_SHELL`
- `AGENT_OBSERVER_NO_SHELL`
- `AGENT_OBSERVER_ACT` / `AGENT_OBSERVER_NO_ACT`
- `AGENT_OBSERVER_REDACT`
- `AGENT_OBSERVER_LOG_FILE`
- `AGENT_OBSERVER_LOG_LEVEL`
- `AGENT_OBSERVER_TELEMETRY`
- `AGENT_OBSERVER_TELEMETRY_ENDPOINT`

Useful production flags:

```bash
agent-observer watch --refresh-interval 2s
agent-observer watch --log-file ~/.local/state/agent-observer/agent-observer.log --log-level info
agent-observer watch --telemetry=on --telemetry-endpoint https://telemetry.example.invalid/events
```

Telemetry is opt-in. No telemetry is sent unless `--telemetry=on` and `--telemetry-endpoint` are both configured.

## CLI Inspection

```bash
go run ./cmd/agent-observer --dump-json
go run ./cmd/agent-observer --dump-json --redact
go run ./cmd/agent-observer --dump-text
go run ./cmd/agent-observer --dump-text --focus active
go run ./cmd/agent-observer --dump-text --focus blocked
go run ./cmd/agent-observer --dump-text --focus warnings
go run ./cmd/agent-observer --diagnostics
go run ./cmd/agent-observer doctor --redact
```

Useful fixture commands:

```bash
make dump-fixture
make dump-active-fixture
make dump-warnings-fixture
make doctor-fixture
```

## Architecture

Stable reusable core for future Techsav reuse:

- `core/schema`: portable snapshot and event vocabulary (schema v2: sessions + providers).
- `core/source`: provider adapter and actor boundaries.
- `core/store`: thread-safe latest-world snapshot store.
- `core/aggregate`: merges per-provider snapshots (session keys namespaced `provider:id`).
- `core/tail`: incremental JSONL tailer (byte offsets, rotation/truncation-safe, cold-start windowing).
- `core/watch`: fsnotify dir watches + bounded hot-file LRU + debounced per-provider dirty sets.
- Snapshot JSON includes `schema_version` so downstream consumers can detect contract changes.

Provider adapters and scheduling:

- `internal/providers/{claude,codex,cursor,manifest}`: source adapters (+ actors for launch/resume).
- `internal/engine`: scan scheduling — watch events, safety poll, manual refresh; dirty-only rescans.
- `internal/claude`: schema-aware task-batch scanner and normalizer.
- Handles `~/.claude/tasks/<batch-id>/<n>.json`.
- Handles `.lock` as batch liveness metadata.
- Handles `.highwatermark` as batch metadata.
- Uses path + size + mtime fingerprint caching.
- Prunes cache entries for files that disappear or become unreadable.
- Skips symlinked task files by default.
- Warns, rather than crashes, on malformed JSON, oversized files, unreadable files, and mid-write reads.

App-specific UI:

- `internal/tui`: Bubble Tea dashboard, terminal windows, PTY shell pane, and screen buffer.
- Bubble Tea code is intentionally not part of the reusable Techsav core.

## Verification

```bash
go test ./...
go build ./...
```

Production checks:

```bash
make test
make race
make vet
make build
make check
make smoke
```

Release snapshot, with GoReleaser installed:

```bash
make snapshot
```

GitHub Actions runs tests, race tests, vet, and build on Linux and macOS.

Release and schema docs:

- [docs/RELEASE.md](docs/RELEASE.md)
- [docs/SCHEMA.md](docs/SCHEMA.md)
- [docs/OPERATIONS.md](docs/OPERATIONS.md)
- [docs/MACOS_SIGNING.md](docs/MACOS_SIGNING.md)
- [SECURITY.md](SECURITY.md)

## Agent Actions

Actions are disabled by default; enable with:

```bash
agent-observer watch --act
```

- Launch (`n`), resume (`R`), and steer runs interactively inside managed PTY panes; stop (`x`) with confirmation.
- The exact command line is always displayed in the pane before and while it runs.
- Commands execute your installed provider CLIs (`claude`, `codex`, `cursor-agent`, manifest `[commands]`) as your user — your existing CLI auth/subscriptions power them. Agent Observer stores no credentials.
- Argv is executed directly, never via a shell; manifest placeholders substitute inside single argv elements, so prompt text cannot inject commands.
- Up to 4 concurrent panes; exited panes keep their buffer and are evicted first.

## Security And Privacy

- Observation is read-only toward provider local state; no model APIs are called.
- Session snapshots and diagnostics may include local paths, prompts, message excerpts, warnings, and timing metadata.
- Do not publish `--dump-json`, `--diagnostics`, or `doctor` output without reviewing it for sensitive local context.
- Use `--redact` before sharing support output.
- The local shell pane is disabled by default; use `--shell` only when a local PTY is appropriate.
- Agent actions are disabled by default; `--act` is an explicit opt-in and `doctor` reports when it is on. See [SECURITY.md](SECURITY.md).
- Telemetry is disabled by default and only sends aggregate event metadata when explicitly enabled.

## Current Limits

- Message history and token usage are not invented; they are omitted until a real local source is identified.
- Shell pane is a practical PTY with partial terminal emulation, not a full xterm implementation yet.
- Role inference is heuristic: lead/manager, review, qa/test, otherwise coding.
