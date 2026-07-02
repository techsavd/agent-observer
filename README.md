# Agent Observer

Terminal-first dashboard for watching local Claude Code shared state. Agent Observer is read-only toward Claude: it watches files, parses local JSON/state, derives batch/task status, and renders a live operations board.

It does not call the Claude API and does not control Claude agents.

## Data Sources

- `~/.claude/tasks`
- `~/.claude/teams` optional

When `teams` is missing, Agent Observer runs in task-batch fallback mode.

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
- `r`: refresh
- `?`: help
- `q`: quit from dashboard focus

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

- `AGENT_OBSERVER_TASKS_DIR`
- `AGENT_OBSERVER_TEAMS_DIR`
- `AGENT_OBSERVER_MAX_FILE_SIZE`
- `AGENT_OBSERVER_REFRESH_INTERVAL`
- `AGENT_OBSERVER_SHELL`
- `AGENT_OBSERVER_NO_SHELL`
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

- `core/schema`: portable snapshot and event vocabulary.
- `core/source`: source adapter boundary and record model.
- `core/store`: derived in-memory state/store concept.
- Snapshot JSON includes `schema_version` so downstream consumers can detect contract changes.

Claude-specific local adapters:

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

## Security And Privacy

- Agent Observer is read-only toward Claude local state.
- Task snapshots and diagnostics may include local paths, prompts, warnings, and timing metadata.
- Do not publish `--dump-json`, `--diagnostics`, or `doctor` output without reviewing it for sensitive local context.
- Use `--redact` before sharing support output.
- The local shell pane is disabled by default; use `--shell` only when a local PTY is appropriate.
- Telemetry is disabled by default and only sends aggregate event metadata when explicitly enabled.

## Current Limits

- Message history and token usage are not invented; they are omitted until a real local source is identified.
- Shell pane is a practical PTY with partial terminal emulation, not a full xterm implementation yet.
- Role inference is heuristic: lead/manager, review, qa/test, otherwise coding.
