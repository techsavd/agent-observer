package tui

import (
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/techsavd/agent-observer/core/source"
)

type fakeActor struct{}

func (fakeActor) CanAct() bool { return true }
func (fakeActor) LaunchArgv(req source.LaunchRequest) ([]string, error) {
	return []string{"/bin/cat"}, nil
}
func (fakeActor) ResumeArgv(sessionID, cwd string) ([]string, error) {
	return []string{"/bin/cat"}, nil
}

func key(model Model, keys ...string) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "ctrl+o":
			msg = tea.KeyMsg{Type: tea.KeyCtrlO}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var updated tea.Model
		updated, cmd = model.Update(msg)
		model = updated.(Model)
	}
	return model, cmd
}

func TestLauncherFlowLaunchSteerStop(t *testing.T) {
	model := testModel().WithActions([]ActionProvider{{Name: "fake", Actor: fakeActor{}}}, true)
	model.width = 140
	model.height = 36

	// n opens the launcher.
	model, _ = key(model, "n")
	if !model.launcher.open {
		t.Fatal("launcher should open on n")
	}
	view := model.View()
	if !strings.Contains(view, "New Run") || !strings.Contains(view, "fake") {
		t.Fatalf("launcher overlay missing:\n%s", view)
	}

	// tab to prompt, type, enter launches.
	model, _ = key(model, "tab", "d", "o", " ", "i", "t")
	if model.launcher.prompt != "do it" {
		t.Fatalf("prompt = %q", model.launcher.prompt)
	}
	model, cmd := key(model, "enter")
	if model.launcher.open {
		t.Fatal("launcher should close on submit")
	}
	if cmd == nil {
		t.Fatal("submit should produce a start command")
	}
	started, ok := cmd().(ptyStartedMsg)
	if !ok || started.err != nil {
		t.Fatalf("start failed: %#v", started)
	}
	updated, readCmd := model.Update(started)
	model = updated.(Model)
	if model.runs.count() != 1 || model.focus != panelShell {
		t.Fatalf("expected one active pane focused, got count=%d focus=%s", model.runs.count(), model.focus)
	}
	defer model.runs.closeAll()
	if readCmd == nil {
		t.Fatal("expected read chain to start")
	}
	view = model.View()
	if !strings.Contains(view, "/bin/cat") {
		t.Fatalf("pane must show exact argv:\n%s", view)
	}

	// Steer: keys route to the PTY while the pane has focus.
	model, _ = key(model, "h", "i", "enter")
	deadline := time.Now().Add(3 * time.Second)
	session := model.runs.activeSession()
	for time.Now().Before(deadline) {
		msg := readPTY(session)().(ptyOutputMsg)
		if msg.text != "" {
			session.write(msg.text)
		}
		if strings.Contains(strings.Join(session.lines(20), "\n"), "hi") {
			break
		}
	}
	if !strings.Contains(strings.Join(session.lines(20), "\n"), "hi") {
		t.Fatalf("steered input not echoed: %v", session.lines(20))
	}

	// ctrl+o detaches, x asks to confirm, y stops.
	model, _ = key(model, "ctrl+o")
	if model.focus == panelShell {
		t.Fatal("ctrl+o should detach focus")
	}
	model, _ = key(model, "x")
	if !model.confirmStop {
		t.Fatal("x should require confirmation")
	}
	view = model.View()
	if !strings.Contains(view, "Confirm Stop") {
		t.Fatalf("confirm overlay missing:\n%s", view)
	}
	model, _ = key(model, "y")
	if model.confirmStop {
		t.Fatal("confirmation should close after y")
	}
	pid := session.cmd.Process.Pid
	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // process fully gone
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("stopped process still alive")
}

func TestActionsHiddenWithoutActFlag(t *testing.T) {
	model := testModel().WithActions([]ActionProvider{{Name: "fake", Actor: fakeActor{}}}, false)
	model.width = 140
	model.height = 36
	model, _ = key(model, "n")
	if model.launcher.open {
		t.Fatal("launcher must not open when actions are disabled")
	}
	if strings.Contains(model.View(), "n new") {
		t.Fatal("action footer must be hidden when disabled")
	}
}

func TestResumeSelectedUsesProviderActor(t *testing.T) {
	model := testModelWithSessions().WithActions([]ActionProvider{{Name: "claude", Actor: fakeActor{}}}, true)
	model.width = 140
	model.height = 36
	model.selected = 0 // busy claude session sorts first
	model, cmd := key(model, "R")
	if cmd == nil {
		t.Fatalf("resume should produce a start command (actionError=%q)", model.actionError)
	}
	started, ok := cmd().(ptyStartedMsg)
	if !ok || started.err != nil {
		t.Fatalf("resume start failed: %#v", started)
	}
	started.session.close()
}
