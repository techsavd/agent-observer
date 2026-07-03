# Security Policy

Agent Observer observes local agent CLI state (Claude Code, Codex, Cursor,
and manifest-defined providers). Observation is strictly read-only: it never
calls model APIs and never writes to provider state directories.

With the opt-in `--act` flag, Agent Observer can also execute provider CLIs
(launch, steer, stop, resume agent runs). See "Agent Actions" below.

## Supported Data Boundary

By default, Agent Observer reads:

- `~/.claude/tasks`, `~/.claude/teams`, `~/.claude/sessions`, `~/.claude/projects`
- `~/.codex/sessions`, `~/.codex/session_index.jsonl`
- `~/.cursor/projects/*/agent-transcripts`
- paths matched by manifest globs in `~/.config/agent-observer/providers`

The shell pane is a real local PTY. It is disabled by default; use `--shell`
only when the local PTY is appropriate.

## Agent Actions (`--act`)

Actions are disabled by default. When enabled with `--act` (or
`AGENT_OBSERVER_ACT=1`):

- The dashboard can execute the provider CLIs found on your PATH (`claude`,
  `codex`, `cursor-agent`, and manifest `[commands]`) as your user, using
  whatever authentication those CLIs already hold (e.g. your subscriptions).
  No API keys are read, stored, or transmitted by Agent Observer itself.
- Nothing executes without an explicit user gesture (`n`, `R`, `x`), and the
  exact argv is displayed in the pane header before and while running.
- Commands are executed directly (argv), never through a shell, and manifest
  placeholder values are substituted within single argv elements so prompt
  text cannot inject additional commands.
- Stopping a run sends SIGTERM, escalating to SIGKILL after 3 seconds, and
  always asks for confirmation first.
- Provider manifests are trusted local configuration: anyone who can write
  `~/.config/agent-observer/providers` can define what a launch command runs.
  Protect that directory like your shell profile.
- `agent-observer doctor` reports when actions are enabled and which CLIs
  would be executed.

## Sensitive Output

The following commands can include local paths, prompts, session titles,
message excerpts, and warning details:

- `agent-observer --dump-json`
- `agent-observer --dump-text`
- `agent-observer --diagnostics`
- `agent-observer doctor`

Use `--redact` before sharing output.

Telemetry is disabled by default. When explicitly enabled, it sends only
aggregate metadata such as command mode, version, platform, counts, warning
count, and timing. It must not include prompts, task titles, session text,
local paths, active file paths, raw warnings, shell transcript, or dump
payloads.

## Reporting Issues

Do not include raw task dumps, prompts, or private repository paths in
public issues. Share redacted diagnostics where possible:

```bash
agent-observer doctor --redact
```
