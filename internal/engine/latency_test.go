package engine

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/store"
	claudeprovider "github.com/techsavd/agent-observer/internal/providers/claude"
)

// TestWatchLatencyEndToEnd appends to a hot transcript and asserts the
// engine pushes an updated snapshot well under the polling interval.
func TestWatchLatencyEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem watcher integration test")
	}
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "-tmp-lat")
	for _, d := range []string{projects, filepath.Join(dir, "sessions"), filepath.Join(dir, "tasks")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	transcript := filepath.Join(projects, "lat-1.jsonl")
	first := `{"type":"user","sessionId":"lat-1","timestamp":"2026-07-02T10:00:00.000Z","cwd":"/tmp/lat","message":{"role":"user","content":"warmup"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := claudeprovider.New(claudeprovider.Config{ClaudeDir: dir})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	e := New([]source.Adapter{adapter}, store.NewMemoryStore(), logger, Config{PollInterval: time.Hour, WatchEnabled: true})

	updates := make(chan schema.WorldSnapshot, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx, func(w schema.WorldSnapshot) { updates <- w })

	select {
	case <-updates: // initial scan marks the transcript hot
	case <-time.After(3 * time.Second):
		t.Fatal("no initial scan")
	}
	time.Sleep(200 * time.Millisecond) // let the hot-file watch settle

	appendAt := time.Now()
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	second := `{"type":"user","sessionId":"lat-1","timestamp":"2026-07-02T10:00:01.000Z","cwd":"/tmp/lat","message":{"role":"user","content":"second"}}` + "\n"
	if _, err := f.WriteString(second); err != nil {
		t.Fatal(err)
	}
	f.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case w := <-updates:
			session, ok := w.Sessions["claude:lat-1"]
			if !ok || session.Turns < 2 {
				continue
			}
			latency := time.Since(appendAt)
			t.Logf("event->snapshot latency: %s (trigger=%s)", latency.Round(time.Millisecond), w.Stats.LastTrigger)
			if w.Stats.LastTrigger != TriggerWatch {
				t.Fatalf("expected watch trigger, got %q", w.Stats.LastTrigger)
			}
			if latency > time.Second {
				t.Fatalf("latency %s exceeds 1s; watch path not effective", latency)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for watched update")
		}
	}
}
