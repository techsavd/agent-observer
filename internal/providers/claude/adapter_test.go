package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

func touchRecent(t *testing.T, path string) {
	t.Helper()
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
}

func writeSessionFile(t *testing.T, dir string, live liveSession) {
	t.Helper()
	payload, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1234.json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanLiveSessionsStatusAndLiveness(t *testing.T) {
	dir := t.TempDir()
	writeSessionFile(t, dir, liveSession{
		PID: 1234, SessionID: "live-busy", CWD: "/tmp/p",
		StartedAt: time.Now().Add(-time.Minute).UnixMilli(),
		Status:    "busy", UpdatedAt: time.Now().UnixMilli(), Name: "demo",
	})
	sessions, warnings := scanLiveSessions(dir, func(int) bool { return true })
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	session := sessions["live-busy"]
	if session.Status != schema.SessionBusy || session.Title != "demo" || session.PID != 1234 {
		t.Fatalf("unexpected session: %+v", session)
	}

	sessions, _ = scanLiveSessions(dir, func(int) bool { return false })
	if sessions["live-busy"].Status != schema.SessionDone {
		t.Fatalf("dead pid must demote to done, got %s", sessions["live-busy"].Status)
	}
}

func TestScanLiveSessionsMissingDirIsQuiet(t *testing.T) {
	sessions, warnings := scanLiveSessions(filepath.Join(t.TempDir(), "absent"), pidAlive)
	if len(sessions) != 0 || len(warnings) != 0 {
		t.Fatalf("missing dir should be quiet, got %v %v", sessions, warnings)
	}
}

func TestTranscriptScannerDerivesSessionDetail(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "claude", "projects", "-tmp-demo-project", "11111111-2222-3333-4444-555555555555.jsonl")
	touchRecent(t, fixture)
	scanner := newTranscriptScanner(filepath.Join("..", "..", "testdata", "claude", "projects"))
	sessions, warnings, _ := scanner.scan()
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	session, ok := sessions["11111111-2222-3333-4444-555555555555"]
	if !ok {
		t.Fatalf("session not derived, got %v", sessions)
	}
	if session.Turns != 3 {
		t.Fatalf("turns = %d, want 3", session.Turns)
	}
	if session.Model != "claude-fable-5" {
		t.Fatalf("model = %q", session.Model)
	}
	if session.LastText != "Tests pass." {
		t.Fatalf("last text = %q", session.LastText)
	}
	if session.CWD != "/tmp/demo-project" {
		t.Fatalf("cwd = %q", session.CWD)
	}
	if session.Tokens == nil || session.Tokens.Input != 200 || session.Tokens.Output != 50 {
		t.Fatalf("tokens = %+v", session.Tokens)
	}
	if !session.Resumable {
		t.Fatal("claude transcript sessions must be resumable")
	}
}

func TestTranscriptScannerIncremental(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "-tmp-x")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, "abc.jsonl")
	line := `{"type":"user","sessionId":"abc","timestamp":"2026-07-01T10:00:00.000Z","cwd":"/tmp/x","message":{"role":"user","content":"first"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	scanner := newTranscriptScanner(dir)
	sessions, _, _ := scanner.scan()
	if sessions["abc"].Turns != 1 || sessions["abc"].LastText != "first" {
		t.Fatalf("unexpected first scan: %+v", sessions["abc"])
	}
	second := `{"type":"user","sessionId":"abc","timestamp":"2026-07-01T10:01:00.000Z","cwd":"/tmp/x","message":{"role":"user","content":"second"}}` + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(second); err != nil {
		t.Fatal(err)
	}
	f.Close()
	sessions, _, _ = scanner.scan()
	if sessions["abc"].Turns != 2 || sessions["abc"].LastText != "second" {
		t.Fatalf("unexpected incremental scan: %+v", sessions["abc"])
	}
}

func TestAdapterSnapshotMergesLiveAndTranscript(t *testing.T) {
	claudeDir := t.TempDir()
	sessionsDir := filepath.Join(claudeDir, "sessions")
	projectDir := filepath.Join(claudeDir, "projects", "-tmp-y")
	for _, dir := range []string{sessionsDir, projectDir, filepath.Join(claudeDir, "tasks")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	transcript := `{"type":"assistant","sessionId":"sess-1","timestamp":"2026-07-01T10:00:00.000Z","cwd":"/tmp/y","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"working on it"}],"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "sess-1.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	live, err := json.Marshal(liveSession{PID: os.Getpid(), SessionID: "sess-1", CWD: "/tmp/y", Status: "busy", Name: "merge-test", StartedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "999.json"), live, 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := New(Config{ClaudeDir: claudeDir})
	snap := adapter.Snapshot(context.Background())
	session, ok := snap.Sessions["sess-1"]
	if !ok {
		t.Fatalf("session missing: %v", snap.Sessions)
	}
	if session.Status != schema.SessionBusy {
		t.Fatalf("status = %s, want busy (live overlay)", session.Status)
	}
	if session.Model != "claude-fable-5" || session.LastText != "working on it" {
		t.Fatalf("transcript detail lost: %+v", session)
	}
	if session.Title != "merge-test" {
		t.Fatalf("live title lost: %+v", session)
	}
}
