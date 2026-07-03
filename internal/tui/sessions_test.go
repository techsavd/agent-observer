package tui

import (
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func startTestSession(t *testing.T, argv []string) *ptySession {
	t.Helper()
	msg := startRun("run-t", strings.Join(argv, " "), "test", argv, t.TempDir(), 60, 12)()
	started, ok := msg.(ptyStartedMsg)
	if !ok || started.err != nil {
		t.Fatalf("start failed: %#v", msg)
	}
	t.Cleanup(started.session.close)
	return started.session
}

// drain runs the read chain synchronously until match appears or times out.
func drain(t *testing.T, s *ptySession, match string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := readPTY(s)
		if cmd == nil {
			break
		}
		msg, ok := cmd().(ptyOutputMsg)
		if !ok {
			break
		}
		if msg.text != "" {
			s.write(msg.text)
		}
		if msg.err != nil {
			s.markClosed(msg.err)
			break
		}
		if strings.Contains(strings.Join(s.lines(50), "\n"), match) {
			return
		}
	}
	if !strings.Contains(strings.Join(s.lines(50), "\n"), match) {
		t.Fatalf("output %q not found in:\n%s", match, strings.Join(s.lines(50), "\n"))
	}
}

func TestPTYSessionEchoesThroughCat(t *testing.T) {
	session := startTestSession(t, []string{"/bin/cat"})
	if err := session.writeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello-pty")}); err != nil {
		t.Fatal(err)
	}
	if err := session.writeKey(tea.KeyMsg{Type: tea.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	drain(t, session, "hello-pty", 3*time.Second)
}

func TestPTYSessionEchoesArgvHeader(t *testing.T) {
	session := startTestSession(t, []string{"/bin/cat"})
	if !strings.Contains(strings.Join(session.lines(10), "\n"), "$ /bin/cat") {
		t.Fatalf("expected argv header, got %v", session.lines(10))
	}
}

func TestStopEscalatesToSigkill(t *testing.T) {
	// The child ignores SIGTERM; stop must still kill it via SIGKILL.
	session := startTestSession(t, []string{"/bin/sh", "-c", `trap "" TERM; echo ready; sleep 300`})
	drain(t, session, "ready", 3*time.Second)
	session.stop()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(session.cmd.Process.Pid, 0); err != nil {
			return // process gone
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("process survived stop escalation")
}

func TestSessionManagerEvictsOnlyExited(t *testing.T) {
	manager := newSessionManager()
	for i := 0; i < maxManagedSessions; i++ {
		if err := manager.add(&ptySession{id: "live-" + strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.add(&ptySession{id: "overflow"}); err == nil {
		t.Fatal("expected error when all panes are live")
	}
	manager.get("live-0").closed = true
	if err := manager.add(&ptySession{id: "after-evict"}); err != nil {
		t.Fatalf("expected exited pane eviction, got %v", err)
	}
	if manager.get("live-0") != nil {
		t.Fatal("exited pane should have been evicted")
	}
	if manager.activeSession().id != "after-evict" {
		t.Fatalf("new session should be active, got %s", manager.activeSession().id)
	}
}

func TestSessionManagerCycle(t *testing.T) {
	manager := newSessionManager()
	for _, id := range []string{"a", "b", "c"} {
		if err := manager.add(&ptySession{id: id}); err != nil {
			t.Fatal(err)
		}
	}
	if manager.activeSession().id != "c" {
		t.Fatalf("active = %s", manager.activeSession().id)
	}
	manager.cycle(1)
	if manager.activeSession().id != "a" {
		t.Fatalf("cycle wrapped to %s", manager.activeSession().id)
	}
	manager.cycle(-1)
	if manager.activeSession().id != "c" {
		t.Fatalf("cycle back to %s", manager.activeSession().id)
	}
	manager.remove("c")
	if manager.activeSession().id == "c" {
		t.Fatal("removed session still active")
	}
}
