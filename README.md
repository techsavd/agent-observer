# Agent Observer

Terminal dashboard for watching local Claude shared state, with an optional local shell pane.

## Run

```bash
go run ./cmd/agent-observer --debug
```

Or from any project:

```bash
"/Users/dsouzas/Documents/New project/scripts/agent-observer" --debug
```

## Keys

- `tab` / `shift+tab`: move dashboard focus
- `j` / `k`: move selection
- `i`: toggle inactive batches
- `s`: open/focus `shell://local`
- `ctrl+o`: detach keyboard focus from shell back to dashboard
- `ctrl+d`: exit shell when shell is focused
- `r`: refresh
- `?`: help
- `q`: quit from dashboard focus

## Shell Pane

The shell pane is a real local PTY. It is not Claude control and does not call the Claude API.

This build includes a small terminal screen buffer that handles common ANSI/CSI behavior: cursor movement, clear screen/line, alternate-screen reset, tabs, backspace, and resize. It is still not a full xterm implementation with color/cell attributes, but fullscreen tools should render much more coherently than a raw log stream.

## Data Sources

- `~/.claude/tasks`
- `~/.claude/teams` optional

## Verify

```bash
go test ./...
go build ./...
```
