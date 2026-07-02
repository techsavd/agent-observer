package manifest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

func TestLoadParsesValidAndReportsInvalid(t *testing.T) {
	manifests, errs := Load(filepath.Join("..", "..", "testdata", "manifests"))
	if len(manifests) != 1 || manifests[0].Name != "aider" {
		t.Fatalf("manifests = %+v", manifests)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want one for broken.toml", errs)
	}
	m := manifests[0]
	if m.Session.Status.BusyWithin.Duration != 30*time.Second {
		t.Fatalf("busy_within = %s", m.Session.Status.BusyWithin.Duration)
	}
	if len(m.Commands.Launch) != 3 || m.Commands.Launch[2] != "{prompt}" {
		t.Fatalf("launch = %v", m.Commands.Launch)
	}
}

func TestValidateRejectsShellStrings(t *testing.T) {
	m := Manifest{
		Name:     "bad",
		Watch:    WatchConfig{Globs: []string{"/tmp/x/*.jsonl"}},
		Commands: CommandsConfig{Launch: []string{"aider --message {prompt} && rm -rf /"}},
	}
	if err := m.validate(); err == nil {
		t.Fatal("expected shell-string command to be rejected")
	}
}

func TestExtractField(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"message":{"content":[{"type":"text","text":"hello"}]},"cwd":"/tmp/p"}`), &value); err != nil {
		t.Fatal(err)
	}
	if got := extractField(value, "cwd"); got != "/tmp/p" {
		t.Fatalf("cwd = %q", got)
	}
	if got := extractField(value, "message.content.text"); got != "hello" {
		t.Fatalf("text = %q", got)
	}
	if got := extractField(value, "missing.path"); got != "" {
		t.Fatalf("missing = %q", got)
	}
}

func TestManifestAdapterObservesFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess-42.jsonl")
	line := `{"cwd":"/tmp/aider-proj","message":{"content":[{"type":"text","text":"working"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(Manifest{
		Name:  "aider",
		Watch: WatchConfig{Globs: []string{filepath.Join(dir, "*.jsonl")}},
		Session: SessionConfig{
			ID:   "filename_stem",
			CWD:  "field:cwd",
			Text: "field:message.content.text",
		},
		Commands: CommandsConfig{Resume: []string{"aider", "--resume", "{session_id}"}},
	})
	snap := adapter.Snapshot(context.Background())
	session, ok := snap.Sessions["sess-42"]
	if !ok {
		t.Fatalf("session missing: %v", snap.Sessions)
	}
	if session.Provider != "aider" || session.CWD != "/tmp/aider-proj" || session.LastText != "working" {
		t.Fatalf("unexpected session: %+v", session)
	}
	if session.Status != schema.SessionBusy {
		t.Fatalf("status = %s, want busy for fresh file", session.Status)
	}
	if !session.Resumable {
		t.Fatal("resume command present, session must be resumable")
	}
}
