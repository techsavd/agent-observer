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

const (
	panelRuns    = "runs"
	panelTasks   = "tasks"
	panelDetails = "details"
	panelHealth  = "health"
	panelShell   = "shell"

	runsStaticRows   = 1
	tasksStaticRows  = 1
	healthStaticRows = 1
)

type Model struct {
	world        schema.WorldSnapshot
	refresh      func(context.Context) schema.WorldSnapshot
	refreshEvery time.Duration
	width        int
	height       int

	focus           string
	selected        int
	taskSelected    int
	warningSelected int
	runOffset       int
	taskOffset      int
	detailOffset    int
	healthOffset    int
	showInactive    bool
	showHelp        bool
	filtering       bool
	filter          string
	debug           bool
	watchMode       bool

	allowShell    bool
	shell         *shellSession
	shellStarting bool
	shellError    error
}

type refreshMsg schema.WorldSnapshot

// runRow is one selectable row in the Runs panel: either a provider session
// or a legacy task batch.
type runRow struct {
	kind    string // "session" or "batch"
	session schema.SessionSnapshot
	batch   schema.BatchSnapshot
}

const (
	rowSession = "session"
	rowBatch   = "batch"
)

type rect struct {
	x, y int
	w, h int
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type dashboardLayout struct {
	bodyH   int
	footerY int
	panels  map[string]rect
}

type footerAction struct {
	label string
	kind  string
	start int
	end   int
}

type panelRender struct {
	title    string
	subtitle string
	lines    []string
	w        int
	h        int
	focused  bool
	border   lipgloss.Color
	bg       lipgloss.Color
	scroll   int
}

func New(world schema.WorldSnapshot, debug bool, refresh func(context.Context) schema.WorldSnapshot) Model {
	return Model{
		world:        world,
		refresh:      refresh,
		refreshEvery: time.Second,
		focus:        panelRuns,
		debug:        debug,
	}
}

func NewWatch(world schema.WorldSnapshot, debug bool, refresh func(context.Context) schema.WorldSnapshot) Model {
	model := New(world, debug, refresh)
	model.watchMode = true
	return model
}

func (m Model) WithRefreshInterval(interval time.Duration) Model {
	if interval > 0 {
		m.refreshEvery = interval
	}
	return m
}

func (m Model) WithShellEnabled(enabled bool) Model {
	m.allowShell = enabled
	if !enabled && m.shell != nil {
		m.shell.close()
		m.shell = nil
		m.shellStarting = false
		if m.focus == panelShell {
			m.focus = panelTasks
		}
	}
	return m
}

func (m Model) Init() tea.Cmd {
	interval := m.refreshEvery
	if interval <= 0 {
		interval = time.Second
	}
	return tea.Tick(interval, func(time.Time) tea.Msg {
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
		m.clamp()
		m.resizeShell()
	case tea.MouseMsg:
		cmd := m.handleMouse(msg)
		m.clamp()
		return m, cmd
	case tea.KeyMsg:
		if m.focus == panelShell && !m.filtering {
			return m.updateShellKey(msg)
		}
		if m.filtering {
			return m.updateFilterKey(msg)
		}
		cmd := m.updateDashboardKey(msg)
		m.clamp()
		return m, cmd
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
		m.focus = panelShell
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

func (m *Model) updateDashboardKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.shell != nil {
			m.shell.close()
		}
		return tea.Quit
	case "esc":
		if m.showHelp {
			m.showHelp = false
			return nil
		}
		if m.filter != "" {
			m.filter = ""
			m.filtering = false
			m.taskSelected = 0
			return nil
		}
	case "?":
		m.showHelp = !m.showHelp
	case "/":
		m.filtering = true
	case "d":
		m.debug = !m.debug
	case "i":
		m.showInactive = !m.showInactive
		m.selected = 0
		m.taskSelected = 0
	case "tab", "right", "l":
		m.nextFocus()
	case "shift+tab", "left", "h":
		m.prevFocus()
	case "1":
		m.focus = panelRuns
	case "2":
		m.focus = panelTasks
	case "3":
		m.focus = panelDetails
	case "4":
		m.focus = m.rightPanelID()
	case "w":
		if !m.shellVisible() {
			m.focus = panelHealth
		}
	case "enter":
		if m.focus == panelTasks {
			m.focus = panelDetails
		} else if m.focus == panelDetails {
			m.focus = panelTasks
		}
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown":
		m.page(1)
	case "pgup":
		m.page(-1)
	case "home":
		m.home()
	case "end":
		m.end()
	case "s":
		return m.openShell()
	case "r":
		return m.refreshCmd()
	}
	return nil
}

func (m *Model) updateFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.shell != nil {
			m.shell.close()
		}
		return *m, tea.Quit
	case "esc":
		m.filter = ""
		m.filtering = false
	case "enter":
		m.filtering = false
	case "backspace", "ctrl+h":
		runes := []rune(m.filter)
		if len(runes) > 0 {
			m.filter = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		m.filter = ""
	default:
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
		}
	}
	m.selected = 0
	m.taskSelected = 0
	m.clamp()
	return *m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	event := tea.MouseEvent(msg)
	layout := m.dashboardLayout()
	if event.Action != tea.MouseActionPress {
		return nil
	}
	if event.Y == layout.footerY {
		return m.handleFooterClick(event.X)
	}
	for id, panelRect := range layout.panels {
		if !panelRect.contains(event.X, event.Y) {
			continue
		}
		if event.IsWheel() {
			if event.Button == tea.MouseButtonWheelUp {
				m.scrollPanel(id, -3)
			} else if event.Button == tea.MouseButtonWheelDown {
				m.scrollPanel(id, 3)
			}
			if id != panelShell || m.shellVisible() {
				m.focus = id
			}
			return nil
		}
		if event.Button != tea.MouseButtonLeft {
			return nil
		}
		m.focus = id
		m.handlePanelClick(id, panelRect, event.X, event.Y)
		return nil
	}
	return nil
}

func (m *Model) handlePanelClick(id string, panelRect rect, _, y int) {
	line := y - panelRect.y - 2
	if line < 0 {
		return
	}
	switch id {
	case panelRuns:
		index := m.runOffset + line - runsStaticRows
		if index >= 0 && index < len(m.runRows()) {
			m.selected = index
			m.taskSelected = 0
		}
	case panelTasks:
		batch, ok := m.selectedBatch()
		if !ok {
			return
		}
		index := m.taskOffset + line - tasksStaticRows
		tasks := m.tasks(batch.BatchID)
		if index >= 0 && index < len(tasks) {
			m.taskSelected = index
		}
	case panelHealth:
		index := m.healthOffset + line - healthStaticRows
		warnings := m.warnings()
		if index >= 0 && index < len(warnings) {
			m.warningSelected = index
		}
	}
}

func (m *Model) handleFooterClick(x int) tea.Cmd {
	_, actions := m.footerTextAndActions()
	for _, action := range actions {
		if x < action.start || x > action.end {
			continue
		}
		switch action.kind {
		case "focus":
			m.nextFocus()
		case "filter":
			m.filtering = true
		case "inactive":
			m.showInactive = !m.showInactive
			m.selected = 0
			m.taskSelected = 0
		case "refresh":
			return m.refreshCmd()
		case "help":
			m.showHelp = !m.showHelp
		case "shell":
			return m.openShell()
		}
		return nil
	}
	return nil
}

func (m Model) View() string {
	m.clamp()
	header := m.header()
	body := m.body()
	footer, _ := m.footerTextAndActions()
	w, _ := m.size()
	footer = trunc(footer, w)
	footerText := footer
	if colorEnabled() {
		footerText = lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Render(footer)
	}
	out := lipgloss.JoinVertical(lipgloss.Left, header, body, footerText)
	if m.showHelp {
		out += "\n" + m.helpOverlay()
	}
	return out
}

func (m Model) header() string {
	w, _ := m.size()
	title := "Agent Observer"
	if colorEnabled() {
		title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render(title)
	}
	filter := "off"
	if m.filter != "" || m.filtering {
		filter = fmt.Sprintf("%q", m.filter)
		if m.filtering {
			filter += " editing"
		}
	}
	debug := ""
	if m.debug {
		debug = "  debug on"
	}
	mode := "mode:" + m.modeLabel()
	line := fmt.Sprintf("%s  %s", title, mode)
	metrics := trunc(fmt.Sprintf("sessions %d/%d  runs %d/%d  tasks %d  blocked %d  warn %d  files %d  scan %s  filter %s%s",
		m.activeSessions(),
		len(m.world.Sessions),
		len(m.activeBatches()),
		len(m.world.Batches),
		len(m.world.Tasks),
		m.blockedCount(),
		len(m.world.Warnings),
		m.world.Stats.FilesScanned,
		m.world.Stats.LastDuration.Round(time.Millisecond),
		filter,
		debug,
	), w)
	if colorEnabled() {
		metrics = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(metrics)
	}
	return line + "\n" + metrics
}

func (m Model) body() string {
	layout := m.dashboardLayout()
	if m.compactLayout() {
		runs := layout.panels[panelRuns]
		tasks := layout.panels[panelTasks]
		details := layout.panels[panelDetails]
		rightID := m.rightPanelID()
		right := layout.panels[rightID]
		return lipgloss.JoinVertical(
			lipgloss.Left,
			m.batchPane(runs.w, runs.h),
			m.taskPane(tasks.w, tasks.h),
			m.detailPane(details.w, details.h),
			m.rightPane(rightID, right.w, right.h),
		)
	}
	runs := layout.panels[panelRuns]
	tasks := layout.panels[panelTasks]
	details := layout.panels[panelDetails]
	rightID := m.rightPanelID()
	right := layout.panels[rightID]
	rightColumn := lipgloss.JoinVertical(lipgloss.Left, m.detailPane(details.w, details.h), m.rightPane(rightID, right.w, right.h))
	return lipgloss.JoinHorizontal(lipgloss.Top, m.batchPane(runs.w, runs.h), m.taskPane(tasks.w, tasks.h), rightColumn)
}

func (m Model) batchPane(w, h int) string {
	return renderPanel(panelRender{
		title:    "1 Runs",
		subtitle: m.runSubtitle(),
		lines:    m.runLines(w),
		w:        w,
		h:        h,
		focused:  m.focus == panelRuns,
		border:   lipgloss.Color("39"),
		scroll:   m.runOffset,
	})
}

func (m Model) taskPane(w, h int) string {
	subtitle := "waiting"
	if session, ok := m.selectedSession(); ok {
		subtitle = trunc(first(session.Title, session.ID), 18)
	} else if batch, ok := m.selectedBatch(); ok {
		subtitle = trunc(batch.BatchID, 18)
	}
	return renderPanel(panelRender{
		title:    "2 Tasks",
		subtitle: subtitle,
		lines:    m.taskLines(w),
		w:        w,
		h:        h,
		focused:  m.focus == panelTasks,
		border:   m.selectedBatchBorder(),
		scroll:   m.taskOffset,
	})
}

func (m Model) detailPane(w, h int) string {
	return renderPanel(panelRender{
		title:    "3 Details",
		subtitle: m.detailSubtitle(),
		lines:    m.detailLines(w),
		w:        w,
		h:        h,
		focused:  m.focus == panelDetails,
		border:   lipgloss.Color("105"),
		scroll:   m.detailOffset,
	})
}

func (m Model) rightPane(id string, w, h int) string {
	if id == panelShell {
		return m.shellPane(w, h)
	}
	return renderPanel(panelRender{
		title:    "4 Health",
		subtitle: fmt.Sprintf("warn %d", len(m.world.Warnings)),
		lines:    m.healthLines(w),
		w:        w,
		h:        h,
		focused:  m.focus == panelHealth,
		border:   lipgloss.Color("214"),
		scroll:   m.healthOffset,
	})
}

func (m Model) shellPane(w, h int) string {
	lines := []string{"local PTY only; not Claude control", "ctrl+o dashboard  ctrl+d exit"}
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
		lines = append(lines, m.shell.lines(max(1, h-5))...)
	}
	return renderPanel(panelRender{
		title:    "4 Local Shell",
		subtitle: "shell://local " + subtitle,
		lines:    lines,
		w:        w,
		h:        h,
		focused:  m.focus == panelShell,
		border:   lipgloss.Color("240"),
		bg:       lipgloss.Color(""),
		scroll:   0,
	})
}

func renderPanel(p panelRender) string {
	p.h = max(4, p.h)
	p.w = max(20, p.w)
	styleW := max(12, p.w-2)
	lineW := max(10, styleW-2)
	contentH := max(1, p.h-2)
	viewport := contentViewport(p.h)
	scroll := clamp(p.scroll, 0, max(0, len(p.lines)-viewport))
	subtitle := p.subtitle
	if len(p.lines) > viewport {
		if subtitle != "" {
			subtitle += " "
		}
		subtitle += fmt.Sprintf("scroll:%d/%d", scroll+1, max(1, len(p.lines)-viewport+1))
	}
	title := p.title
	if subtitle != "" {
		title += "  " + subtitle
	}
	prefix := "  "
	border := p.border
	if p.focused {
		prefix = "> "
		border = lipgloss.Color("86")
	}
	title = prefix + trunc(title, lineW)
	body := []string{title}
	for _, line := range viewportLines(p.lines, scroll, viewport) {
		line = trunc(line, lineW)
		body = append(body, styleSelectedLine(line, p.focused))
	}
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(styleW).Height(contentH)
	if !colorEnabled() {
		style = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1).Width(styleW).Height(contentH)
	} else if string(p.bg) != "" && !lowColorMode() {
		style = style.Background(p.bg)
	}
	return style.Render(strings.Join(body, "\n"))
}

func box(title string, lines []string, w, h int, focused bool) string {
	return boxWithTheme(title, lines, w, h, focused, lipgloss.Color("240"), lipgloss.Color(""))
}

func boxWithTheme(title string, lines []string, w, h int, focused bool, border lipgloss.Color, background lipgloss.Color) string {
	return renderPanel(panelRender{title: title, lines: lines, w: w, h: h, focused: focused, border: border, bg: background})
}

func (m Model) runLines(w int) []string {
	mode := "active only"
	if m.showInactive {
		mode = "all runs"
	}
	if m.filter != "" {
		mode += " filtered"
	}
	lines := []string{fmt.Sprintf("view %-8s total %-2d live %-2d", mode, len(m.world.Sessions)+len(m.world.Batches), m.activeSessions()+len(m.activeBatches()))}
	rows := m.runRows()
	if len(rows) == 0 {
		if m.watchMode {
			return append(lines, "No active run yet.", "Start an agent session now.", "Waiting for provider files.")
		}
		return append(lines, "No runs match this view.", "Press i for history or esc to clear filters.")
	}
	for i, row := range rows {
		var line string
		if row.kind == rowSession {
			session := row.session
			line = fmt.Sprintf("%-6s %-4s %-12s %s",
				trunc(session.Provider, 6),
				sessionStatusShort(session.Status),
				trunc(first(session.Title, shortPath(session.CWD), session.ID), 12),
				age(session.LastUpdated),
			)
		} else {
			b := row.batch
			label := "REC"
			if !inactiveBatch(b) {
				label = "CUR"
			}
			line = fmt.Sprintf("%-3s %-12s %-4s r%d w%d b%d e%d d%d",
				label,
				trunc(b.BatchID, 12),
				batchStatusShort(b),
				b.Counts.Running,
				b.Counts.Waiting,
				b.Counts.Blocked,
				b.Counts.Errored,
				b.Counts.Completed,
			)
		}
		lines = append(lines, selectableLine(trunc(line, max(20, w-6)), i == m.selected, m.focus == panelRuns))
	}
	return lines
}

// shortPath renders a cwd as its last two path elements for narrow panes.
func shortPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

func (m Model) taskLines(w int) []string {
	if session, ok := m.selectedSession(); ok {
		width := max(20, w-6)
		lines := []string{fmt.Sprintf("%s session %s", session.Provider, sessionStatusShort(session.Status))}
		lines = append(lines, "cwd    "+first(session.CWD, "-"))
		lines = append(lines, "model  "+first(session.Model, "-"))
		lines = append(lines, fmt.Sprintf("turns  %d", session.Turns))
		if session.LastText != "" {
			lines = append(lines, "", "LAST MESSAGE")
			lines = append(lines, wrapText(session.LastText, width)...)
		}
		return lines
	}
	batch, ok := m.selectedBatch()
	if !ok {
		return []string{"No agent session or team detected yet.", "Start a Claude Code, Codex, or Cursor", "agent task now.", "This pane will populate automatically."}
	}
	tasks := m.tasks(batch.BatchID)
	lines := []string{"status     role      age       files  task"}
	if len(tasks) == 0 {
		return append(lines, "No tasks match the current filter.")
	}
	for i, t := range tasks {
		title := first(t.ActiveForm, t.Title, t.ID)
		role := first(string(t.Role), "-")
		line := fmt.Sprintf("%-10s %-9s %-9s %-5d %s",
			plainStatus(t.Status),
			trunc(role, 9),
			age(t.LastUpdated),
			len(t.ActiveFiles),
			trunc(title, max(14, w-45)),
		)
		lines = append(lines, selectableLine(trunc(line, max(20, w-6)), i == m.taskSelected, m.focus == panelTasks))
	}
	return lines
}

func (m Model) detailLines(w int) []string {
	if session, ok := m.selectedSession(); ok {
		return m.sessionDetailLines(session, w)
	}
	task, ok := m.selectedTask()
	if !ok {
		return []string{"Select a run and task to inspect status, files, and source metadata."}
	}
	width := max(20, w-6)
	lines := []string{
		"title   " + first(task.Title, task.ActiveForm, task.ID),
		fmt.Sprintf("status  %-10s role %s", string(task.Status), first(string(task.Role), "-")),
		"batch   " + task.BatchID,
		"updated " + age(task.LastUpdated),
	}
	if task.SourcePath != "" {
		lines = append(lines, "source  "+task.SourcePath)
	}
	if strings.TrimSpace(task.ActiveForm) != "" {
		lines = append(lines, "", "ACTIVE")
		lines = append(lines, wrapText(task.ActiveForm, width)...)
	}
	if strings.TrimSpace(task.Description) != "" {
		lines = append(lines, "", "DESCRIPTION")
		lines = append(lines, wrapText(task.Description, width)...)
	}
	lines = append(lines, "", "FILES")
	if len(task.ActiveFiles) == 0 {
		lines = append(lines, "-")
	} else {
		for _, file := range task.ActiveFiles {
			lines = append(lines, "- "+file.Path)
		}
	}
	return lines
}

func (m Model) sessionDetailLines(session schema.SessionSnapshot, w int) []string {
	width := max(20, w-6)
	lines := []string{
		"session  " + session.ID,
		fmt.Sprintf("provider %-8s status %s", session.Provider, string(session.Status)),
		"cwd      " + first(session.CWD, "-"),
		"model    " + first(session.Model, "-"),
		fmt.Sprintf("turns    %d", session.Turns),
		"updated  " + age(session.LastUpdated),
	}
	if session.PID > 0 {
		lines = append(lines, fmt.Sprintf("pid      %d", session.PID))
	}
	if session.Tokens != nil {
		lines = append(lines, fmt.Sprintf("tokens   in %d out %d", session.Tokens.Input, session.Tokens.Output))
	}
	if session.Resumable {
		lines = append(lines, "resume   available")
	}
	if session.SourcePath != "" {
		lines = append(lines, "source   "+session.SourcePath)
	}
	if session.LastText != "" {
		lines = append(lines, "", "LAST MESSAGE")
		lines = append(lines, wrapText(session.LastText, width)...)
	}
	return lines
}

func (m Model) healthLines(w int) []string {
	stats := m.world.Stats
	lines := []string{fmt.Sprintf("warn %-2d cache %-3d partial %-2d oversized %-2d symlink %-2d",
		len(m.world.Warnings),
		stats.CacheHits,
		stats.PartialRetries,
		stats.SkippedOversize,
		stats.SkippedSymlinks,
	)}
	warnings := m.warnings()
	if len(warnings) == 0 {
		return append(lines, "No warnings in the current view.")
	}
	for i, warning := range warnings {
		source := first(warning.SourcePath, "-")
		line := fmt.Sprintf("WARN %-24s %s", trunc(source, 24), warning.Message)
		lines = append(lines, selectableLine(trunc(line, max(20, w-6)), i == m.warningSelected, m.focus == panelHealth))
	}
	return lines
}

func selectableLine(line string, selected, _ bool) string {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	return prefix + line
}

func styleSelectedLine(line string, focused bool) string {
	if !colorEnabled() || !strings.HasPrefix(line, "> ") {
		return line
	}
	style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	if focused && !lowColorMode() {
		style = style.Background(lipgloss.Color("30"))
	} else {
		style = style.Foreground(lipgloss.Color("86"))
	}
	return style.Render(line)
}

func (m Model) footerTextAndActions() (string, []footerAction) {
	w, _ := m.size()
	compact := w < 110
	parts := []struct {
		label string
		kind  string
	}{
		{label: "tab focus", kind: "focus"},
		{label: "1 runs", kind: ""},
		{label: "2 tasks", kind: ""},
		{label: "3 details", kind: ""},
		{label: "4 " + m.rightPanelID(), kind: ""},
		{label: "/ filter", kind: "filter"},
		{label: "i inactive", kind: "inactive"},
		{label: "r refresh", kind: "refresh"},
		{label: "? help", kind: "help"},
	}
	if compact {
		parts = []struct {
			label string
			kind  string
		}{
			{label: "tab", kind: "focus"},
			{label: "1 runs", kind: ""},
			{label: "2 tasks", kind: ""},
			{label: "3 detail", kind: ""},
			{label: "4 " + m.rightPanelID(), kind: ""},
			{label: "/", kind: "filter"},
			{label: "i", kind: "inactive"},
			{label: "r", kind: "refresh"},
			{label: "?", kind: "help"},
		}
	}
	if m.allowShell {
		shellLabel := "s shell"
		detachLabel := "ctrl+o detach shell"
		if compact {
			shellLabel = "s"
			detachLabel = "ctrl+o"
		}
		parts = append(parts, struct {
			label string
			kind  string
		}{label: shellLabel, kind: "shell"})
		parts = append(parts, struct {
			label string
			kind  string
		}{label: detachLabel, kind: ""})
	}
	quitLabel := "q quit"
	if compact {
		quitLabel = "q"
	}
	parts = append(parts, struct {
		label string
		kind  string
	}{label: quitLabel, kind: ""})

	text := "focus:" + m.focusLabel() + " | "
	actions := []footerAction{}
	for i, part := range parts {
		if i > 0 {
			text += "  "
		}
		start := len(text)
		text += part.label
		end := len(text) - 1
		if part.kind != "" {
			actions = append(actions, footerAction{label: part.label, kind: part.kind, start: start, end: end})
		}
	}
	return text, actions
}

func (m Model) helpOverlay() string {
	lines := []string{
		"Global: q quit, r refresh, ? help, esc closes help or clears filter.",
		"Navigation: tab/shift+tab or left/right moves focus; 1/2/3/4 jump to panels.",
		"Selection: arrows or j/k move rows; pgup/pgdown page; home/end jump.",
		"Details: enter toggles between tasks and details; / filters runs, tasks, and warnings.",
		"Mouse: click focuses/selects rows; wheel scrolls the hovered panel; footer labels are clickable.",
	}
	if m.allowShell {
		lines = append(lines, "Shell: s opens local shell://local; ctrl+o returns to dashboard; ctrl+d exits shell.")
	}
	w, _ := m.size()
	return boxWithTheme("Help", lines, clamp(w-4, 60, 100), 9, true, lipgloss.Color("86"), lipgloss.Color("235"))
}

func (m Model) dashboardLayout() dashboardLayout {
	w, h := m.size()
	bodyH := max(16, h-4)
	layout := dashboardLayout{bodyH: bodyH, footerY: 2 + bodyH, panels: map[string]rect{}}
	if m.compactLayout() {
		runsH, tasksH, detailsH, rightH := compactPaneHeights(bodyH)
		paneW := max(20, w-4)
		y := 2
		layout.panels[panelRuns] = rect{x: 0, y: y, w: paneW, h: runsH}
		y += runsH
		layout.panels[panelTasks] = rect{x: 0, y: y, w: paneW, h: tasksH}
		y += tasksH
		layout.panels[panelDetails] = rect{x: 0, y: y, w: paneW, h: detailsH}
		y += detailsH
		layout.panels[m.rightPanelID()] = rect{x: 0, y: y, w: paneW, h: rightH}
		return layout
	}
	leftW := clamp(w/4, 28, 36)
	rightW := clamp(w/3, 38, 50)
	mainW := max(38, w-leftW-rightW)
	rightX := leftW + mainW
	detailH := clamp(bodyH/2, 8, max(8, bodyH-6))
	layout.panels[panelRuns] = rect{x: 0, y: 2, w: leftW, h: bodyH}
	layout.panels[panelTasks] = rect{x: leftW, y: 2, w: mainW, h: bodyH}
	layout.panels[panelDetails] = rect{x: rightX, y: 2, w: rightW, h: detailH}
	layout.panels[m.rightPanelID()] = rect{x: rightX, y: 2 + detailH, w: rightW, h: bodyH - detailH}
	return layout
}

func compactPaneHeights(total int) (int, int, int, int) {
	total = max(16, total)
	runsH := clamp(total/5, 4, 7)
	detailsH := clamp(total/4, 4, 8)
	rightH := clamp(total/5, 4, 7)
	tasksH := total - runsH - detailsH - rightH
	if tasksH < 4 {
		deficit := 4 - tasksH
		tasksH = 4
		for deficit > 0 && detailsH > 4 {
			detailsH--
			deficit--
		}
		for deficit > 0 && rightH > 4 {
			rightH--
			deficit--
		}
		for deficit > 0 && runsH > 4 {
			runsH--
			deficit--
		}
	}
	return runsH, tasksH, detailsH, rightH
}

func (m Model) compactLayout() bool {
	w, _ := m.size()
	return w < 120
}

func (m Model) size() (int, int) {
	w, h := m.width, m.height
	if w == 0 {
		w = 120
	}
	if h == 0 {
		h = 36
	}
	return w, h
}

func (m Model) rightPanelID() string {
	if m.shellVisible() {
		return panelShell
	}
	return panelHealth
}

func (m Model) runSubtitle() string {
	return fmt.Sprintf("%d active / %d total", m.activeSessions()+len(m.activeBatches()), len(m.world.Sessions)+len(m.world.Batches))
}

func (m Model) detailSubtitle() string {
	if session, ok := m.selectedSession(); ok {
		return fmt.Sprintf("%s %s", string(session.Status), session.Provider)
	}
	task, ok := m.selectedTask()
	if !ok {
		return "none selected"
	}
	return fmt.Sprintf("%s %s", string(task.Status), first(string(task.Role), "-"))
}

func (m Model) shellVisible() bool {
	return m.shell != nil || m.shellStarting || m.shellError != nil
}

func (m *Model) move(delta int) {
	switch m.normalizedFocus() {
	case panelTasks:
		m.taskSelected += delta
	case panelHealth:
		m.warningSelected += delta
	case panelDetails:
		m.scrollPanel(panelDetails, delta)
	default:
		m.selected += delta
		m.taskSelected = 0
	}
}

func (m *Model) page(delta int) {
	amount := max(1, m.viewportHeight(m.normalizedFocus())-1)
	switch m.normalizedFocus() {
	case panelRuns, panelTasks, panelHealth:
		m.move(delta * amount)
	case panelDetails:
		m.scrollPanel(panelDetails, delta*amount)
	default:
		m.scrollPanel(m.normalizedFocus(), delta*amount)
	}
}

func (m *Model) home() {
	switch m.normalizedFocus() {
	case panelTasks:
		m.taskSelected = 0
	case panelHealth:
		m.warningSelected = 0
	case panelDetails:
		m.detailOffset = 0
	default:
		m.selected = 0
		m.taskSelected = 0
	}
}

func (m *Model) end() {
	switch m.normalizedFocus() {
	case panelTasks:
		if batch, ok := m.selectedBatch(); ok {
			m.taskSelected = len(m.tasks(batch.BatchID)) - 1
		}
	case panelHealth:
		m.warningSelected = len(m.warnings()) - 1
	case panelDetails:
		m.detailOffset = len(m.detailLines(80))
	default:
		m.selected = len(m.runRows()) - 1
		m.taskSelected = 0
	}
}

func (m *Model) scrollPanel(id string, delta int) {
	switch id {
	case panelRuns:
		m.runOffset += delta
	case panelTasks:
		m.taskOffset += delta
	case panelDetails:
		m.detailOffset += delta
	case panelHealth:
		m.healthOffset += delta
	}
}

func (m *Model) clamp() {
	m.focus = m.normalizedFocus()
	rows := m.runRows()
	if len(rows) == 0 {
		m.selected = 0
		m.taskSelected = 0
	} else {
		m.selected = clamp(m.selected, 0, len(rows)-1)
		if batch, ok := m.selectedBatch(); ok {
			tasks := m.tasks(batch.BatchID)
			if len(tasks) == 0 {
				m.taskSelected = 0
			} else {
				m.taskSelected = clamp(m.taskSelected, 0, len(tasks)-1)
			}
		} else {
			m.taskSelected = 0
		}
	}
	warnings := m.warnings()
	if len(warnings) == 0 {
		m.warningSelected = 0
	} else {
		m.warningSelected = clamp(m.warningSelected, 0, len(warnings)-1)
	}
	m.clampScrolls()
}

func (m *Model) clampScrolls() {
	layout := m.dashboardLayout()
	m.runOffset = clamp(m.runOffset, 0, max(0, len(m.runLines(80))-m.viewportHeight(panelRuns)))
	if m.focus == panelRuns {
		m.runOffset = ensureVisible(m.runOffset, m.selected+runsStaticRows, m.viewportHeight(panelRuns))
	}
	m.taskOffset = clamp(m.taskOffset, 0, max(0, len(m.taskLines(100))-m.viewportHeight(panelTasks)))
	if m.focus == panelTasks {
		m.taskOffset = ensureVisible(m.taskOffset, m.taskSelected+tasksStaticRows, m.viewportHeight(panelTasks))
	}
	detailW := 80
	if panelRect, ok := layout.panels[panelDetails]; ok {
		detailW = panelRect.w
	}
	m.detailOffset = clamp(m.detailOffset, 0, max(0, len(m.detailLines(detailW))-m.viewportHeight(panelDetails)))
	if m.rightPanelID() == panelHealth {
		m.healthOffset = clamp(m.healthOffset, 0, max(0, len(m.healthLines(80))-m.viewportHeight(panelHealth)))
		if m.focus == panelHealth {
			m.healthOffset = ensureVisible(m.healthOffset, m.warningSelected+healthStaticRows, m.viewportHeight(panelHealth))
		}
	}
}

func (m Model) viewportHeight(id string) int {
	if panelRect, ok := m.dashboardLayout().panels[id]; ok {
		return contentViewport(panelRect.h)
	}
	return 1
}

func ensureVisible(offset, selected, viewport int) int {
	if viewport <= 0 {
		return offset
	}
	if selected < offset {
		return selected
	}
	if selected >= offset+viewport {
		return selected - viewport + 1
	}
	return offset
}

func (m Model) normalizedFocus() string {
	switch m.focus {
	case panelRuns, "batches":
		return panelRuns
	case panelTasks:
		return panelTasks
	case panelDetails:
		return panelDetails
	case panelHealth:
		if m.shellVisible() {
			return panelShell
		}
		return panelHealth
	case panelShell:
		if m.shellVisible() {
			return panelShell
		}
		return panelHealth
	default:
		return panelRuns
	}
}

func (m Model) focusLabel() string {
	if m.filtering {
		return m.normalizedFocus() + " filter"
	}
	return m.normalizedFocus()
}

func (m *Model) nextFocus() {
	foci := m.focusOrder()
	current := m.normalizedFocus()
	for i, focus := range foci {
		if focus == current {
			m.focus = foci[(i+1)%len(foci)]
			return
		}
	}
	m.focus = foci[0]
}

func (m *Model) prevFocus() {
	foci := m.focusOrder()
	current := m.normalizedFocus()
	for i, focus := range foci {
		if focus == current {
			m.focus = foci[(i+len(foci)-1)%len(foci)]
			return
		}
	}
	m.focus = foci[0]
}

func (m Model) focusOrder() []string {
	return []string{panelRuns, panelTasks, panelDetails, m.rightPanelID()}
}

func (m *Model) openShell() tea.Cmd {
	if !m.allowShell {
		return nil
	}
	if m.shell != nil && !m.shell.closed {
		m.focus = panelShell
		return nil
	}
	m.shellStarting = true
	m.shellError = nil
	m.focus = panelShell
	cols, rows := m.shellSize()
	return startShell(m.shellCWD(), cols, rows)
}

func (m Model) refreshCmd() tea.Cmd {
	if m.refresh == nil {
		return nil
	}
	return func() tea.Msg { return refreshMsg(m.refresh(context.Background())) }
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

func (m Model) shellSize() (int, int) {
	layout := m.dashboardLayout()
	if panelRect, ok := layout.panels[panelShell]; ok {
		return max(20, panelRect.w-4), max(5, panelRect.h-4)
	}
	_, h := m.size()
	return 40, max(8, h-10)
}

func (m Model) resizeShell() {
	if m.shell != nil {
		c, r := m.shellSize()
		m.shell.resize(c, r)
	}
}

func (m Model) updateShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+o" {
		m.focus = panelTasks
		return m, nil
	}
	if m.shell == nil || m.shell.closed {
		m.focus = panelTasks
		return m, nil
	}
	if err := m.shell.writeKey(msg); err != nil {
		m.shellError = err
	}
	return m, nil
}

// runRows lists sessions first (busy before idle before done), then batches
// in their existing priority order. Both honor the inactive toggle and filter.
func (m Model) runRows() []runRow {
	rows := make([]runRow, 0, len(m.world.Sessions)+len(m.world.Batches))
	for _, session := range m.sessions() {
		rows = append(rows, runRow{kind: rowSession, session: session})
	}
	for _, batch := range m.batches() {
		rows = append(rows, runRow{kind: rowBatch, batch: batch})
	}
	return rows
}

func (m Model) sessions() []schema.SessionSnapshot {
	out := make([]schema.SessionSnapshot, 0, len(m.world.Sessions))
	for _, session := range m.world.Sessions {
		if !m.showInactive && inactiveSession(session) {
			continue
		}
		if !m.sessionMatches(session) {
			continue
		}
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := sessionPriority(out[i].Status), sessionPriority(out[j].Status)
		if left != right {
			return left < right
		}
		if !out[i].LastUpdated.Equal(out[j].LastUpdated) {
			return out[i].LastUpdated.After(out[j].LastUpdated)
		}
		return out[i].Provider+out[i].ID < out[j].Provider+out[j].ID
	})
	return out
}

func (m Model) activeSessions() int {
	count := 0
	for _, session := range m.world.Sessions {
		if !inactiveSession(session) {
			count++
		}
	}
	return count
}

func (m Model) sessionMatches(session schema.SessionSnapshot) bool {
	query := m.filterQuery()
	if query == "" {
		return true
	}
	fields := []string{
		session.ID,
		session.Provider,
		session.Title,
		session.CWD,
		session.Model,
		session.LastText,
		string(session.Status),
	}
	for _, field := range fields {
		if containsFold(field, query) {
			return true
		}
	}
	return false
}

func inactiveSession(session schema.SessionSnapshot) bool {
	return session.Status == schema.SessionDone
}

func sessionPriority(status schema.SessionStatus) int {
	switch status {
	case schema.SessionBusy:
		return 0
	case schema.SessionIdle:
		return 1
	case schema.SessionUnknown:
		return 2
	default:
		return 3
	}
}

func sessionStatusShort(status schema.SessionStatus) string {
	switch status {
	case schema.SessionBusy:
		return "BUSY"
	case schema.SessionIdle:
		return "IDLE"
	case schema.SessionDone:
		return "DONE"
	default:
		return "?"
	}
}

func (m Model) batches() []schema.BatchSnapshot {
	out := make([]schema.BatchSnapshot, 0, len(m.world.Batches))
	for _, b := range m.world.Batches {
		if !m.showInactive && inactiveBatch(b) {
			continue
		}
		if !m.batchMatches(b) {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		return priority(out[i]) < priority(out[j]) || (priority(out[i]) == priority(out[j]) && out[i].LastUpdated.After(out[j].LastUpdated))
	})
	return out
}

func (m Model) activeBatches() []schema.BatchSnapshot {
	active := make([]schema.BatchSnapshot, 0)
	for _, batch := range m.world.Batches {
		if !inactiveBatch(batch) {
			active = append(active, batch)
		}
	}
	return active
}

func (m Model) selectedRun() (runRow, bool) {
	rows := m.runRows()
	if len(rows) == 0 {
		return runRow{}, false
	}
	index := clamp(m.selected, 0, len(rows)-1)
	return rows[index], true
}

func (m Model) selectedBatch() (schema.BatchSnapshot, bool) {
	row, ok := m.selectedRun()
	if !ok || row.kind != rowBatch {
		return schema.BatchSnapshot{}, false
	}
	return row.batch, true
}

func (m Model) selectedSession() (schema.SessionSnapshot, bool) {
	row, ok := m.selectedRun()
	if !ok || row.kind != rowSession {
		return schema.SessionSnapshot{}, false
	}
	return row.session, true
}

func (m Model) selectedTask() (schema.TaskSnapshot, bool) {
	batch, ok := m.selectedBatch()
	if !ok {
		return schema.TaskSnapshot{}, false
	}
	tasks := m.tasks(batch.BatchID)
	if len(tasks) == 0 {
		return schema.TaskSnapshot{}, false
	}
	index := clamp(m.taskSelected, 0, len(tasks)-1)
	return tasks[index], true
}

func (m Model) tasks(batchID string) []schema.TaskSnapshot {
	out := []schema.TaskSnapshot{}
	batchMatches := strings.Contains(strings.ToLower(batchID), m.filterQuery())
	for _, t := range m.world.Tasks {
		if t.BatchID != batchID {
			continue
		}
		if !batchMatches && !m.taskMatches(t) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := taskPriority(out[i].Status), taskPriority(out[j].Status)
		if left != right {
			return left < right
		}
		if !out[i].LastUpdated.Equal(out[j].LastUpdated) {
			return out[i].LastUpdated.After(out[j].LastUpdated)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m Model) warnings() []schema.WarningSnapshot {
	query := m.filterQuery()
	if query == "" {
		return append([]schema.WarningSnapshot{}, m.world.Warnings...)
	}
	out := []schema.WarningSnapshot{}
	for _, warning := range m.world.Warnings {
		if containsFold(warning.SourcePath, query) || containsFold(warning.Message, query) {
			out = append(out, warning)
		}
	}
	return out
}

func (m Model) filterQuery() string {
	return strings.ToLower(strings.TrimSpace(m.filter))
}

func (m Model) batchMatches(batch schema.BatchSnapshot) bool {
	query := m.filterQuery()
	if query == "" {
		return true
	}
	if containsFold(batch.BatchID, query) {
		return true
	}
	for _, task := range m.world.Tasks {
		if task.BatchID == batch.BatchID && m.taskMatches(task) {
			return true
		}
	}
	return false
}

func (m Model) taskMatches(task schema.TaskSnapshot) bool {
	query := m.filterQuery()
	if query == "" {
		return true
	}
	fields := []string{
		task.ID,
		task.BatchID,
		task.Title,
		task.Description,
		task.ActiveForm,
		string(task.Status),
		string(task.Role),
		task.SourcePath,
	}
	for _, field := range fields {
		if containsFold(field, query) {
			return true
		}
	}
	for _, file := range task.ActiveFiles {
		if containsFold(file.Path, query) {
			return true
		}
	}
	return false
}

func containsFold(value, query string) bool {
	return strings.Contains(strings.ToLower(value), query)
}

func (m Model) selectedBatchBorder() lipgloss.Color {
	if session, ok := m.selectedSession(); ok {
		return sessionBorder(session.Status)
	}
	if batch, ok := m.selectedBatch(); ok {
		return batchBorder(batch)
	}
	return lipgloss.Color("244")
}

func sessionBorder(status schema.SessionStatus) lipgloss.Color {
	switch status {
	case schema.SessionBusy:
		return lipgloss.Color("82")
	case schema.SessionIdle:
		return lipgloss.Color("220")
	default:
		return lipgloss.Color("244")
	}
}

func (m Model) blockedCount() int {
	total := 0
	for _, batch := range m.world.Batches {
		total += batch.Counts.Blocked + batch.Counts.Errored
	}
	return total
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

func inactiveBatch(batch schema.BatchSnapshot) bool {
	return batch.Counts.Running == 0 && batch.Counts.Waiting == 0 && batch.Counts.Blocked == 0 && batch.Counts.Errored == 0
}

func (m Model) modeLabel() string {
	if m.watchMode {
		return "watch"
	}
	return "dashboard"
}

func plainStatus(status schema.TaskStatus) string {
	return strings.ToUpper(string(status))
}

func batchStatusShort(batch schema.BatchSnapshot) string {
	switch {
	case batch.Counts.Running > 0:
		return "RUN"
	case batch.Counts.Waiting > 0:
		return "WAIT"
	case batch.Counts.Blocked > 0:
		return "BLK"
	case batch.Counts.Errored > 0:
		return "ERR"
	default:
		return "DONE"
	}
}

func batchBorder(batch schema.BatchSnapshot) lipgloss.Color {
	switch {
	case batch.Counts.Running > 0:
		return lipgloss.Color("82")
	case batch.Counts.Waiting > 0:
		return lipgloss.Color("220")
	case batch.Counts.Blocked > 0 || batch.Counts.Errored > 0:
		return lipgloss.Color("196")
	default:
		return lipgloss.Color("244")
	}
}

func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

func lowColorMode() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_OBSERVER_LOW_COLOR")))
	return value == "1" || value == "true" || value == "yes"
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

func contentViewport(panelHeight int) int {
	return max(1, panelHeight-3)
}

func viewportLines(lines []string, offset, limit int) []string {
	if len(lines) == 0 || limit <= 0 {
		return nil
	}
	offset = clamp(offset, 0, max(0, len(lines)-1))
	end := clamp(offset+limit, offset, len(lines))
	return lines[offset:end]
}

func wrapText(value string, width int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	width = max(12, width)
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}
	lines := []string{}
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	lines = append(lines, current)
	return lines
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
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

func filesLimited(files []schema.ActiveFile, width int) string {
	if len(files) == 0 {
		return "-"
	}
	out := []string{}
	for _, f := range files {
		out = append(out, f.Path)
	}
	return trunc(strings.Join(out, ", "), width)
}
