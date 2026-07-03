package tail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func linesToStrings(lines [][]byte) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = string(line)
	}
	return out
}

func TestLinesReadsCompleteLinesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\ntwo\npartial")
	tailer := New()
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got %v, want [one two]", got)
	}
}

func TestLinesEmitsHeldPartialWhenCompleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\npar")
	tailer := New()
	if _, err := tailer.Lines(path); err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, "tial\nnext\n")
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	if len(got) != 2 || got[0] != "partial" || got[1] != "next" {
		t.Fatalf("got %v, want [partial next]", got)
	}
}

func TestLinesOnlyNewLinesOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\n")
	tailer := New()
	if _, err := tailer.Lines(path); err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, "two\n")
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	if len(got) != 1 || got[0] != "two" {
		t.Fatalf("got %v, want [two]", got)
	}
}

func TestLinesUnchangedFileReturnsNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\n")
	tailer := New()
	if _, err := tailer.Lines(path); err != nil {
		t.Fatal(err)
	}
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("got %v, want none", linesToStrings(lines))
	}
}

func TestLinesTruncationResets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\ntwo\n")
	tailer := New()
	if _, err := tailer.Lines(path); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "x\n")
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	if len(got) != 1 || got[0] != "x" {
		t.Fatalf("got %v, want [x]", got)
	}
}

func TestLinesRotationByInodeResets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\n")
	tailer := New()
	if _, err := tailer.Lines(path); err != nil {
		t.Fatal(err)
	}
	// Replace with a different file of the same size (atomic-rename rewrite).
	other := filepath.Join(dir, "b.jsonl")
	writeFile(t, other, "two\n")
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	if len(got) != 1 || got[0] != "two" {
		t.Fatalf("got %v, want [two] after rotation", got)
	}
}

func TestLinesOversizedPartialDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	tailer := New()
	tailer.MaxPartial = 8
	writeFile(t, path, strings.Repeat("x", 32))
	if _, err := tailer.Lines(path); err == nil {
		t.Fatal("expected oversize-partial error")
	}
	// Once the line completes, tailing resumes with the next lines.
	appendFile(t, path, "\nok\n")
	lines, err := tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	if len(got) != 1 || got[0] != "ok" {
		t.Fatalf("got %v, want [ok]", got)
	}
}

func TestTailFromSkipsHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "old1\nold2\nrecent1\nrecent2\n")
	tailer := New()
	lines, err := tailer.TailFrom(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	got := linesToStrings(lines)
	// Seeking 16 bytes from the end lands mid-line; the partial first line
	// must be discarded, leaving only complete trailing lines.
	if len(got) != 1 || got[0] != "recent2" {
		t.Fatalf("got %v, want [recent2]", got)
	}
	// Subsequent appends flow through Lines as usual.
	appendFile(t, path, "more\n")
	lines, err = tailer.Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	got = linesToStrings(lines)
	if len(got) != 1 || got[0] != "more" {
		t.Fatalf("got %v, want [more]", got)
	}
}

func TestPruneDropsUnseenFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	writeFile(t, path, "one\n")
	tailer := New()
	if _, err := tailer.Lines(path); err != nil {
		t.Fatal(err)
	}
	tailer.Prune(map[string]bool{})
	if len(tailer.states) != 0 {
		t.Fatalf("expected pruned state, got %d entries", len(tailer.states))
	}
}
