package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

func fixtureAdapter(t *testing.T) *Adapter {
	t.Helper()
	adapter := New(Config{CodexDir: filepath.Join("..", "..", "testdata", "codex")})
	// Fixtures carry a fixed date; disable age pruning so tests stay stable.
	adapter.maxAge = 1000000 * time.Hour
	return adapter
}

func touchRecent(t *testing.T, path string) {
	t.Helper()
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
}

func TestCodexSnapshotFoldsRolloutEvents(t *testing.T) {
	adapter := fixtureAdapter(t)
	rollout := filepath.Join("..", "..", "testdata", "codex", "sessions", "2026", "06", "30",
		"rollout-2026-06-30T10-00-00-019f0000-aaaa-bbbb-cccc-ddddeeee0001.jsonl")
	touchRecent(t, rollout)
	snap := adapter.Snapshot(context.Background())
	if len(snap.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", snap.Warnings)
	}
	session, ok := snap.Sessions["019f0000-aaaa-bbbb-cccc-ddddeeee0001"]
	if !ok {
		t.Fatalf("session missing: %v", snap.Sessions)
	}
	if session.CWD != "/tmp/codex-project" {
		t.Fatalf("cwd = %q", session.CWD)
	}
	if session.Model != "gpt-5.3-codex" {
		t.Fatalf("model = %q", session.Model)
	}
	if session.Status != schema.SessionIdle {
		t.Fatalf("status = %s, want idle (task_complete, recent mtime)", session.Status)
	}
	if session.LastText != "Refactoring billing now." {
		t.Fatalf("last text = %q", session.LastText)
	}
	if session.Tokens == nil || session.Tokens.Input != 5000 || session.Tokens.Output != 900 {
		t.Fatalf("tokens = %+v (token_count totals must replace, not sum)", session.Tokens)
	}
	if !session.Resumable {
		t.Fatal("codex sessions must be resumable")
	}
}

func TestCodexOldRolloutIsDone(t *testing.T) {
	adapter := fixtureAdapter(t)
	rollout := filepath.Join("..", "..", "testdata", "codex", "sessions", "2026", "06", "30",
		"rollout-2026-06-30T10-00-00-019f0000-aaaa-bbbb-cccc-ddddeeee0001.jsonl")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(rollout, old, old); err != nil {
		t.Fatal(err)
	}
	snap := adapter.Snapshot(context.Background())
	session := snap.Sessions["019f0000-aaaa-bbbb-cccc-ddddeeee0001"]
	if session.Status != schema.SessionDone {
		t.Fatalf("status = %s, want done for stale rollout", session.Status)
	}
}

func TestSessionIDFromFilename(t *testing.T) {
	got := sessionIDFromFilename("/x/rollout-2026-06-30T10-00-00-019f0000-aaaa-bbbb-cccc-ddddeeee0001.jsonl")
	if got != "019f0000-aaaa-bbbb-cccc-ddddeeee0001" {
		t.Fatalf("id = %q", got)
	}
}

func TestCodexMissingDirIsQuiet(t *testing.T) {
	adapter := New(Config{CodexDir: filepath.Join(t.TempDir(), "absent")})
	snap := adapter.Snapshot(context.Background())
	if len(snap.Sessions) != 0 || len(snap.Warnings) != 0 {
		t.Fatalf("expected quiet empty snapshot, got %v %v", snap.Sessions, snap.Warnings)
	}
}
