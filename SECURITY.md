# Security Policy

Agent Observer is a local read-only observer for Claude Code task state. It does not call the Claude API and does not send telemetry.

## Supported Data Boundary

By default, Agent Observer reads:

- `~/.claude/tasks`
- `~/.claude/teams` when present

The shell pane is a real local PTY. It is disabled by default; use `--shell` only when the local PTY is appropriate.

## Sensitive Output

The following commands can include local paths, prompts, task titles, and warning details:

- `agent-observer --dump-json`
- `agent-observer --dump-text`
- `agent-observer --diagnostics`
- `agent-observer doctor`

Use `--redact` before sharing output.

Telemetry is disabled by default. When explicitly enabled, it sends only aggregate metadata such as command mode, version, platform, counts, warning count, and timing. It must not include prompts, task titles, local paths, active file paths, raw warnings, shell transcript, or dump payloads.

## Reporting Issues

Do not include raw task dumps, prompts, or private repository paths in public issues. Share redacted diagnostics where possible:

```bash
agent-observer doctor --redact
```
