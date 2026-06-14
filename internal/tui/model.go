package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/techsavd/agent-observer/core/schema"
)

type Model struct {
	world         schema.WorldSnapshot
	refresh       func(context.Context) schema.WorldSnapshot
	width         int
	height        int
	focus         string
	selected      int
	taskSelected  int
	showInactive  bool
	showHelp      bool
	debug         bool
	shell         *shellSession
	shellStarting bool
	shellError    error
}

type refreshMsg schema.WorldSnapshot

func New(world schema.WorldSnapshot, debug bool, refresh func(context.Context) schema.WorldSnapshot) Model {
	return Model{world: world, refresh: refresh, focus: "batches", debug: debug}
}

func (m Model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		if m.refresh == nil {
			return nil
		}
		return refreshMsg(m.refresh(context.Background()))
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeShell()
	case tea.MouseMsg:
		if m.shell != nil && tea.MouseEvent(msg).Action == tea.MouseActionPress && tea.MouseEvent(msg).X > m.width-45 {
			m.focus = "shell"
		}
	case tea.KeyMsg:
		if m.focus == "shell" {
			return m.updateShellKey(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			if m.shell != nil {
				m.shell.close()
			}
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
		case "d":
			m.debug = !m.debug
		case "i":
			m.showInactive = !m.showInactive
			m.clamp()
		case "tab":
			m.nextFocus()
		case "shift+tab":
			m.prevFocus()
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "s":
			if m.shell != nil && !m.shell.closed {
				m.focus = "shell"
				return m, nil
			}
			m.shellStarting = true
			cols, rows := m.shellSize()
			return m, startShell(m.shellCWD(), cols, rows)
		case "r":
			if m.refresh != nil {
				return m, func() tea.Msg { return refreshMsg(m.refresh(context.Background())) }
			}
		}
	case refreshMsg:
		m.world = schema.WorldSnapshot(msg)
		m.clamp()
		return m, m.Init()
	case shellStartedMsg:
		m.shellStarting = false
		if msg.err != nil {
			m.shellError = msg.err
			return m, nil
		}
		m.shell = msg.session
		m.shellError = nil
		m.focus = "shell"
		m.resizeShell()
		return m, readShell(m.shell)
	case shellOutputMsg:
		if m.shell == nil {
			return m, nil
		}
		if msg.text != "" {
			m.shell.write(msg.text)
			return m, readShell(m.shell)
		}
		if msg.err != nil {
			m.shell.markClosed(msg.err)
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		m.width = 120
	}
	if m.height == 0 {
		m.height = 36
	}
	header := lipgloss.NewStyle().Bold(true).Render("Agent Observer") + "\n" +
		fmt.Sprintf("batches:%d warnings:%d files:%d refresh:%s", len(m.world.Batches), len(m.world.Warnings), m.world.Stats.FilesScanned, m.world.Stats.LastDuration.Round(time.Millisecond))
	body := m.body()
	footer := "tab focus  j/k move  s shell  ctrl+o detach shell  i inactive  d debug  r refresh  ? help  q quit"
	if m.focus != "" {
		footer = "focus:" + m.focus + " | " + footer
	}
	out := lipgloss.JoinVertical(lipgloss.Left, header, body, lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(footer))
	if m.showHelp {
		out += "\n" + box("help", []string{"s open/focus shell://local", "ctrl+o leave shell focus", "ctrl+d exits shell", "q quits from dashboard focus"}, 60, 8, true)
	}
	return out
}

func (m Model) body() string {
	w, h := m.width, max(16, m.height-4)
	if w < 110 {
		if m.shell != nil || m.shellStarting || m.shellError != nil {
			return lipgloss.JoinVertical(lipgloss.Left, m.batchPane(w-4, 6), m.taskPane(w-4, 6), m.rightPane(w-4, max(12, h-10)))
		}
		return lipgloss.JoinVertical(lipgloss.Left, m.batchPane(w-4, 8), m.taskPane(w-4, max(8, h-18)), m.rightPane(w-4, 8))
	}
	leftW, rightW := 34, 44
	mainW := max(40, w-leftW-rightW-4)
	return lipgloss.JoinHorizontal(lipgloss.Top, m.batchPane(leftW, h), m.taskPane(mainW, h), m.rightPane(rightW, h))
}

func (m Model) batchPane(w, h int) string {
	lines := []string{fmt.Sprintf("$ batches inactive=%t", m.showInactive)}
	batches := m.batches()
	if len(batches) == 0 {
		lines = append(lines, "no active batches visible", "press i to reveal inactive")
	}
	for i, b := range batches {
		prefix := " "
		if i == m.selected {
			prefix = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s run:%d wait:%d block:%d done:%d", prefix, trunc(b.BatchID, 16), b.Counts.Running, b.Counts.Waiting, b.Counts.Blocked, b.Counts.Completed))
	}
	return box("window://batches", lines, w, h, m.focus == "batches")
}

func (m Model) taskPane(w, h int) string {
	batches := m.batches()
	if len(batches) == 0 {
		return box("window://tasks", []string{"no selected batch"}, w, h, m.focus == "tasks")
	}
	b := batches[m.selected]
	tasks := m.tasks(b.BatchID)
	lines := []string{"$ tasks " + b.BatchID}
	for i, t := range tasks {
		prefix := " "
		if i == m.taskSelected {
			prefix = ">"
		}
		lines = append(lines,
			fmt.Sprintf("%s %s [%s/%s]", prefix, trunc(first(t.ActiveForm, t.Title, t.ID), max(20, w-20)), t.Status, t.Role),
			"  files: "+files(t.ActiveFiles),
			"  updated: "+age(t.LastUpdated),
		)
	}
	return box("window://tasks/"+b.BatchID, lines, w, h, m.focus == "tasks")
}

func (m Model) rightPane(w, h int) string {
	if m.shell != nil || m.shellStarting || m.shellError != nil {
		lines := []string{"$ shell://local", "writable local PTY; not Claude control", "ctrl+o dashboard focus; ctrl+d exit shell"}
		subtitle := "open with s"
		if m.shellStarting {
			lines = append(lines, "starting shell...")
		} else if m.shellError != nil {
			lines = append(lines, "error: "+m.shellError.Error())
		} else if m.shell != nil {
			subtitle = trunc(m.shell.cwd, max(12, w-4))
			if m.debug {
				lines = append(lines, fmt.Sprintf("debug raw=%d partial=%q", len(m.shell.raw), trunc(m.shell.partial, 16)))
			}
			lines = append(lines, m.shell.lines(max(1, h-7))...)
		}
		return box("shell://local "+subtitle, lines, w, h, m.focus == "shell")
	}
	lines := []string{"$ health", fmt.Sprintf("warnings=%d", len(m.world.Warnings))}
	for _, warning := range m.world.Warnings {
		lines = append(lines, "WARN "+trunc(warning.SourcePath+": "+warning.Message, max(20, w-8)))
	}
	return box("window://health", lines, w, h, m.focus == "health")
}

func box(title string, lines []string, w, h int, focused bool) string {
	h = max(5, h)
	prefix := "  "
	border := lipgloss.Color("240")
	if focused {
		prefix = "> "
		border = lipgloss.Color("86")
	}
	body := append([]string{prefix + title}, lines...)
	if len(body) > h-2 {
		body = append(body[:h-3], "... truncated")
	}
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(max(20, w)).Height(h).Render(strings.Join(body, "\n"))
}

func (m Model) batches() []schema.BatchSnapshot {
	out := make([]schema.BatchSnapshot, 0, len(m.world.Batches))
	for _, b := range m.world.Batches {
		if !m.showInactive && b.Counts.Running+b.Counts.Waiting+b.Counts.Blocked+b.Counts.Errored == 0 {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		return priority(out[i]) < priority(out[j]) || (priority(out[i]) == priority(out[j]) && out[i].LastUpdated.After(out[j].LastUpdated))
	})
	return out
}

func (m Model) tasks(batchID string) []schema.TaskSnapshot {
	out := []schema.TaskSnapshot{}
	for _, t := range m.world.Tasks {
		if t.BatchID == batchID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return taskPriority(out[i].Status) < taskPriority(out[j].Status) })
	return out
}

func (m *Model) move(delta int) {
	if m.focus == "tasks" {
		m.taskSelected += delta
	} else {
		m.selected += delta
		m.taskSelected = 0
	}
	m.clamp()
}

func (m *Model) clamp() {
	m.selected = clamp(m.selected, 0, max(0, len(m.batches())-1))
	if len(m.batches()) > 0 {
		m.taskSelected = clamp(m.taskSelected, 0, max(0, len(m.tasks(m.batches()[m.selected].BatchID))-1))
	}
}

func (m *Model) nextFocus() {
	if m.focus == "batches" {
		m.focus = "tasks"
	} else if m.focus == "tasks" {
		m.focus = "health"
	} else {
		m.focus = "batches"
	}
}
func (m *Model) prevFocus() {
	if m.focus == "health" {
		m.focus = "tasks"
	} else if m.focus == "tasks" {
		m.focus = "batches"
	} else {
		m.focus = "health"
	}
}

func (m Model) shellCWD() string {
	if cwd := os.Getenv("AGENT_OBSERVER_CALLER_CWD"); cwd != "" {
		return cwd
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func (m Model) shellSize() (int, int) { return 40, max(8, m.height-10) }
func (m Model) resizeShell() {
	if m.shell != nil {
		c, r := m.shellSize()
		m.shell.resize(c, r)
	}
}

func (m Model) updateShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+o" {
		m.focus = "tasks"
		return m, nil
	}
	if m.shell == nil || m.shell.closed {
		m.focus = "tasks"
		return m, nil
	}
	if err := m.shell.writeKey(msg); err != nil {
		m.shellError = err
	}
	return m, nil
}

func priority(b schema.BatchSnapshot) int {
	switch {
	case b.Counts.Running > 0:
		return 0
	case b.Counts.Waiting > 0:
		return 1
	case b.Counts.Blocked > 0:
		return 2
	case b.Counts.Errored > 0:
		return 3
	default:
		return 4
	}
}
func taskPriority(s schema.TaskStatus) int {
	switch s {
	case schema.StatusRunning:
		return 0
	case schema.StatusWaiting:
		return 1
	case schema.StatusBlocked:
		return 2
	case schema.StatusErrored:
		return 3
	case schema.StatusCompleted:
		return 4
	default:
		return 5
	}
}
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 4 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "-"
}
func age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return time.Since(t).Round(time.Second).String() + " ago"
}
func files(files []schema.ActiveFile) string {
	if len(files) == 0 {
		return "-"
	}
	out := []string{}
	for _, f := range files {
		out = append(out, f.Path)
	}
	return trunc(strings.Join(out, ", "), 80)
}
