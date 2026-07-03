# Operations

## Health Check

Run:

```bash
agent-observer doctor --redact
```

Use `--dump-json` with `doctor` when another tool needs structured output:

```bash
agent-observer doctor --dump-json --redact
```

## Local Snapshot Testing

Run the smoke test against the current platform binary in `dist/`:

```bash
make smoke
```

Or pass a binary explicitly:

```bash
scripts/smoke-test ./dist/agent-observer_darwin_arm64_v8.0/agent-observer
```

Install the current platform snapshot binary to `~/.local/bin`:

```bash
make install-snapshot
```

Install to another directory:

```bash
scripts/install-snapshot /tmp/agent-observer-bin
```

## Shell Integration

Release archives include static completion files:

- `completions/agent-observer.bash`
- `completions/_agent-observer`
- `completions/agent-observer.fish`

They also include `man/agent-observer.1` for local manual-page installation.

## Logging

Agent Observer is quiet by default. Enable file logging when diagnosing a local installation:

```bash
agent-observer watch \
  --log-file ~/.local/state/agent-observer/agent-observer.log \
  --log-level info
```

Supported log levels are `debug`, `info`, `warn`, and `error`.

## Restricted Environments

The local shell pane is disabled by default. Enable it only when a local PTY is appropriate:

```bash
agent-observer watch --shell
```

Increase refresh interval on slow filesystems:

```bash
agent-observer watch --refresh-interval 5s
```

## Telemetry

Telemetry is opt-in and disabled by default. It only sends aggregate event metadata when both consent and an endpoint are configured:

```bash
agent-observer watch \
  --telemetry=on \
  --telemetry-endpoint https://telemetry.example.invalid/events
```

Telemetry never sends task titles, prompts, source paths, active file paths, raw warnings, shell transcript, or dump payloads.

## Common Issues

- Missing `~/.claude/teams` is expected. The app falls back to task-batch mode.
- Missing or unreadable `~/.claude/tasks` means there is no source data to observe.
- Malformed JSON is reported as a warning and retried on the next refresh.
- Oversized files are skipped according to `--max-file-size`.
- Symlinked task files are skipped by default.
