package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitDirty(t *testing.T, ch <-chan Dirty, want string) Dirty {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case dirty := <-ch:
			if dirty.Providers[want] {
				return dirty
			}
		case <-deadline:
			t.Fatalf("timed out waiting for dirty %q", want)
		}
	}
}

func TestWatcherDetectsNewFileInDir(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem watcher test")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Dirty, 8)
	w, err := Start(ctx, map[string][]string{"claude": {dir}}, 30*time.Millisecond, out)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(dir, "new.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitDirty(t, out, "claude")
}

func TestWatcherDetectsWritesToHotFile(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem watcher test")
	}
	dir := t.TempDir()
	other := t.TempDir() // hot file outside any watched dir
	path := filepath.Join(other, "transcript.jsonl")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Dirty, 8)
	w, err := Start(ctx, map[string][]string{"codex": {dir}}, 30*time.Millisecond, out)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.MarkHot("codex", path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("b\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	waitDirty(t, out, "codex")
}

func TestWatcherHotFileBudget(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Dirty, 8)
	w, err := Start(ctx, map[string][]string{"p": {dir}}, 30*time.Millisecond, out)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.maxHot = 2
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, "f"+string(rune('a'+i))+".jsonl")
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		w.MarkHot("p", path)
	}
	if got := w.HotCount(); got > 2 {
		t.Fatalf("hot count = %d, want <= 2", got)
	}
}

func TestWatcherCoalescesBursts(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem watcher test")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Dirty, 64)
	w, err := Start(ctx, map[string][]string{"claude": {dir}}, 100*time.Millisecond, out)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, "burst.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	waitDirty(t, out, "claude")
	// The burst happened within one debounce window; after draining the
	// first signal there should be at most one trailing signal, not ten.
	time.Sleep(300 * time.Millisecond)
	extra := 0
	for {
		select {
		case <-out:
			extra++
			continue
		default:
		}
		break
	}
	if extra > 2 {
		t.Fatalf("expected coalesced events, got %d extra signals", extra)
	}
}
