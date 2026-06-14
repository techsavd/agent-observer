package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestScreenCursorAddressing(t *testing.T) {
	s := newScreen(20, 5)
	s.write("hello\x1b[2;4Hthere")
	lines := strings.Join(s.lines(5), "\n")
	if !strings.Contains(lines, "hello") || !strings.Contains(lines, "   there") {
		t.Fatalf("expected cursor-addressed text, got:\n%s", lines)
	}
}

func TestScreenClear(t *testing.T) {
	s := newScreen(20, 5)
	s.write("hello\x1b[2Jafter")
	lines := strings.Join(s.lines(5), "\n")
	if strings.Contains(lines, "hello") || !strings.Contains(lines, "after") {
		t.Fatalf("expected clear-screen handling, got:\n%s", lines)
	}
}

func TestShellKeyBytes(t *testing.T) {
	got, ok := keyBytes(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !ok || len(got) != 1 || got[0] != 4 {
		t.Fatalf("expected ctrl+d byte, got %v ok=%v", got, ok)
	}
	got, ok = keyBytes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pwd")})
	if !ok || string(got) != "pwd" {
		t.Fatalf("expected rune bytes, got %q ok=%v", string(got), ok)
	}
}
