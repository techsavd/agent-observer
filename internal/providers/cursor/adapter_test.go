package cursor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

func TestCursorSnapshotDerivesSessionFromTranscript(t *testing.T) {
	adapter := New(Config{CursorDir: filepath.Join("..", "..", "testdata", "cursor")})
	transcript := filepath.Join("..", "..", "testdata", "cursor", "projects", "-tmp-demo",
		"agent-transcripts", "aaaa-bbbb-cccc", "aaaa-bbbb-cccc.jsonl")
	now := time.Now()
	if err := os.Chtimes(transcript, now, now); err != nil {
		t.Fatal(err)
	}
	snap := adapter.Snapshot(context.Background())
	session, ok := snap.Sessions["aaaa-bbbb-cccc"]
	if !ok {
		t.Fatalf("session missing: %v", snap.Sessions)
	}
	if session.Status != schema.SessionBusy {
		t.Fatalf("status = %s, want busy for just-touched transcript", session.Status)
	}
	if session.CWD != "/tmp/demo" {
		t.Fatalf("cwd = %q", session.CWD)
	}
	if session.Turns != 2 {
		t.Fatalf("turns = %d", session.Turns)
	}
	if session.LastText != "Found the race; patching the fixture setup." {
		t.Fatalf("last text = %q", session.LastText)
	}
	if session.Resumable {
		t.Fatal("cursor sessions are not resumable yet")
	}
}

func TestCursorRecencyStatus(t *testing.T) {
	adapter := New(Config{})
	base := time.Now()
	adapter.now = func() time.Time { return base }
	for _, tc := range []struct {
		age  time.Duration
		want schema.SessionStatus
	}{
		{5 * time.Second, schema.SessionBusy},
		{5 * time.Minute, schema.SessionIdle},
		{time.Hour, schema.SessionDone},
	} {
		if got := adapter.statusFromRecency(base.Add(-tc.age)); got != tc.want {
			t.Fatalf("age %s: status = %s, want %s", tc.age, got, tc.want)
		}
	}
}

func TestCursorMissingDirIsQuiet(t *testing.T) {
	adapter := New(Config{CursorDir: filepath.Join(t.TempDir(), "absent")})
	snap := adapter.Snapshot(context.Background())
	if len(snap.Sessions) != 0 || len(snap.Warnings) != 0 {
		t.Fatalf("expected quiet empty snapshot, got %v %v", snap.Sessions, snap.Warnings)
	}
}
