package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/techsavd/agent-observer/core/source"
)

// launcherState is the modal for starting a new agent run: pick a provider,
// type a prompt, confirm the working directory.
type launcherState struct {
	open        bool
	providerIdx int
	field       int // 0 provider, 1 prompt, 2 cwd
	prompt      string
	cwd         string
	err         string
}

func (m *Model) openLauncher() {
	if !m.allowAct || len(m.actors) == 0 {
		return
	}
	cwd := ""
	if session, ok := m.selectedSession(); ok {
		cwd = session.CWD
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	m.launcher = launcherState{open: true, cwd: cwd}
}

func (m *Model) updateLauncherKey(msg tea.KeyMsg) tea.Cmd {
	l := &m.launcher
	switch msg.String() {
	case "esc":
		l.open = false
		return nil
	case "tab", "down":
		l.field = (l.field + 1) % 3
		return nil
	case "shift+tab", "up":
		l.field = (l.field + 2) % 3
		return nil
	case "left":
		if l.field == 0 && len(m.actors) > 0 {
			l.providerIdx = (l.providerIdx + len(m.actors) - 1) % len(m.actors)
		}
		return nil
	case "right":
		if l.field == 0 && len(m.actors) > 0 {
			l.providerIdx = (l.providerIdx + 1) % len(m.actors)
		}
		return nil
	case "enter":
		return m.submitLauncher()
	case "backspace", "ctrl+h":
		value := l.fieldValue()
		runes := []rune(value)
		if len(runes) > 0 {
			l.setFieldValue(string(runes[:len(runes)-1]))
		}
		return nil
	case "ctrl+u":
		l.setFieldValue("")
		return nil
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		text := " "
		if msg.Type == tea.KeyRunes {
			text = string(msg.Runes)
		}
		l.setFieldValue(l.fieldValue() + text)
	}
	return nil
}

func (l *launcherState) fieldValue() string {
	switch l.field {
	case 1:
		return l.prompt
	case 2:
		return l.cwd
	default:
		return ""
	}
}

func (l *launcherState) setFieldValue(value string) {
	switch l.field {
	case 1:
		l.prompt = value
	case 2:
		l.cwd = value
	}
}

func (m *Model) submitLauncher() tea.Cmd {
	l := &m.launcher
	if len(m.actors) == 0 {
		l.err = "no actionable providers"
		return nil
	}
	actor := m.actors[clamp(l.providerIdx, 0, len(m.actors)-1)]
	argv, err := actor.Actor.LaunchArgv(source.LaunchRequest{Prompt: strings.TrimSpace(l.prompt), CWD: l.cwd})
	if err != nil {
		l.err = err.Error()
		return nil
	}
	cwd := strings.TrimSpace(l.cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if info, statErr := os.Stat(cwd); statErr != nil || !info.IsDir() {
		l.err = "cwd is not a directory: " + cwd
		return nil
	}
	l.open = false
	return m.spawnRun(actor.Name, argv, cwd)
}

// resumeSelected relaunches the selected observed session via its provider.
func (m *Model) resumeSelected() tea.Cmd {
	if !m.allowAct {
		return nil
	}
	session, ok := m.selectedSession()
	if !ok || !session.Resumable {
		return nil
	}
	for _, actor := range m.actors {
		if actor.Name != session.Provider {
			continue
		}
		argv, err := actor.Actor.ResumeArgv(session.ID, session.CWD)
		if err != nil {
			m.actionError = err.Error()
			return nil
		}
		cwd := session.CWD
		if info, statErr := os.Stat(cwd); cwd == "" || statErr != nil || !info.IsDir() {
			cwd, _ = os.Getwd()
		}
		return m.spawnRun(actor.Name, argv, cwd)
	}
	m.actionError = "no actionable provider for " + session.Provider
	return nil
}

func (m *Model) spawnRun(provider string, argv []string, cwd string) tea.Cmd {
	id := m.runs.newID()
	title := strings.Join(argv, " ")
	m.focus = panelShell
	m.actionError = ""
	cols, rows := m.shellSize()
	return startRun(id, title, provider, argv, cwd, cols, rows)
}

func (m Model) launcherOverlay() string {
	l := m.launcher
	names := make([]string, len(m.actors))
	for i, actor := range m.actors {
		marker := "  "
		if i == clamp(l.providerIdx, 0, max(0, len(m.actors)-1)) {
			marker = "> "
		}
		names[i] = marker + actor.Name
	}
	cursor := func(field int, value string) string {
		if l.field == field {
			return value + "_"
		}
		return value
	}
	lines := []string{
		"provider  " + strings.Join(names, "   ") + "  (left/right)",
		"prompt    " + cursor(1, l.prompt),
		"cwd       " + cursor(2, l.cwd),
		"",
		"tab switches fields; enter launches; esc cancels",
	}
	if l.err != "" {
		lines = append(lines, "error: "+l.err)
	}
	w, _ := m.size()
	return boxWithTheme("New Run", lines, clamp(w-4, 50, 90), 9, true, lipgloss.Color("86"), lipgloss.Color("235"))
}

func (m Model) confirmStopOverlay() string {
	session := m.runs.activeSession()
	label := "run"
	if session != nil {
		label = fmt.Sprintf("%s (%s)", session.title, session.provider)
	}
	lines := []string{
		"Stop " + label + "?",
		"",
		"y stops it (SIGTERM, then SIGKILL after 3s); n/esc cancels",
	}
	w, _ := m.size()
	return boxWithTheme("Confirm Stop", lines, clamp(w-4, 50, 80), 6, true, lipgloss.Color("196"), lipgloss.Color("235"))
}
