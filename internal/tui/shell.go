package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// ptySession is one managed PTY pane: the local shell or a launched agent
// run. All mutation happens on the Bubble Tea update loop; reads are pumped
// through per-session command chains that deliver ptyOutputMsg.
type ptySession struct {
	id       string
	title    string
	provider string
	argv     []string
	cmd      *exec.Cmd
	file     *os.File
	cwd      string
	screen   *screen
	raw      []string
	partial  string
	closed   bool
	exitCode int
	err      error
	waitOnce sync.Once
}

const providerShell = "shell"

var ansiPattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\a]*(?:\a|\x1b\\)|[()][0-9A-Za-z]|.)`)

type ptyStartedMsg struct {
	session *ptySession
	err     error
}

type ptyOutputMsg struct {
	id   string
	text string
	err  error
}

type ptyExitedMsg struct {
	id       string
	exitCode int
}

// startShell launches the user's login shell as a managed session.
func startShell(id, cwd string, cols, rows int) tea.Cmd {
	shellPath := os.Getenv("SHELL")
	if strings.TrimSpace(shellPath) == "" {
		shellPath = "/bin/sh"
	}
	return startRun(id, "shell://local", providerShell, []string{shellPath}, cwd, cols, rows)
}

// startRun spawns argv on a PTY in cwd. The exact argv is echoed into the
// pane so the user always sees the command being run on their behalf.
func startRun(id, title, provider string, argv []string, cwd string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		if len(argv) == 0 {
			return ptyStartedMsg{err: fmt.Errorf("empty command")}
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "TERM=xterm-256color", "AGENT_OBSERVER_SHELL=1")
		file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(max(20, cols)), Rows: uint16(max(5, rows))})
		if err != nil {
			return ptyStartedMsg{err: err}
		}
		session := &ptySession{
			id:       id,
			title:    title,
			provider: provider,
			argv:     argv,
			cmd:      cmd,
			file:     file,
			cwd:      cwd,
			screen:   newScreen(cols, rows),
			exitCode: -1,
		}
		session.write(fmt.Sprintf("$ %s\r\n", strings.Join(argv, " ")))
		return ptyStartedMsg{session: session}
	}
}

// readPTY reads one chunk; Update re-issues it, forming a per-session chain.
func readPTY(s *ptySession) tea.Cmd {
	if s == nil || s.closed || s.file == nil {
		return nil
	}
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := s.file.Read(buf)
		if n > 0 {
			return ptyOutputMsg{id: s.id, text: string(buf[:n])}
		}
		return ptyOutputMsg{id: s.id, err: err}
	}
}

func (s *ptySession) write(text string) {
	if s == nil {
		return
	}
	if s.screen != nil {
		s.screen.write(text)
	}
	clean := ansiPattern.ReplaceAllString(text, "")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	if clean == "" {
		return
	}
	parts := strings.Split(s.partial+clean, "\n")
	s.partial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		if strings.TrimSpace(line) != "" {
			s.raw = append(s.raw, line)
		}
	}
	if len(s.raw) > 200 {
		s.raw = s.raw[len(s.raw)-200:]
	}
}

func (s *ptySession) lines(limit int) []string {
	if s == nil || s.screen == nil {
		return nil
	}
	lines := append([]string{}, s.raw...)
	if strings.TrimSpace(s.partial) != "" {
		lines = append(lines, s.partial)
	}
	for _, line := range s.screen.lines(limit) {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func (s *ptySession) resize(cols, rows int) {
	if s == nil || s.closed {
		return
	}
	if s.file != nil {
		_ = pty.Setsize(s.file, &pty.Winsize{Cols: uint16(max(20, cols)), Rows: uint16(max(5, rows))})
	}
	if s.screen != nil {
		s.screen.resize(cols, rows)
	}
}

// close hangs up the PTY (plain-shell semantics).
func (s *ptySession) close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.file != nil {
		_ = s.file.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGHUP)
	}
	s.reap()
}

// stop terminates a managed run: SIGTERM, then SIGKILL if it lingers.
func (s *ptySession) stop() {
	if s == nil || s.closed {
		return
	}
	if s.cmd != nil && s.cmd.Process != nil {
		process := s.cmd.Process
		_ = process.Signal(syscall.SIGTERM)
		time.AfterFunc(3*time.Second, func() {
			// Signal on an exited process fails harmlessly.
			_ = process.Signal(syscall.SIGKILL)
		})
		// Reap so the killed child does not linger as a zombie.
		s.reap()
	}
	// The read chain observes EOF/EIO once the process dies and marks the
	// session closed; the buffer stays visible.
}

func (s *ptySession) markClosed(err error) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.file != nil {
		_ = s.file.Close()
	}
	s.reap()
	if err != nil && !isExpectedExitError(err) {
		s.err = err
		s.write("\r\nexited: " + err.Error() + "\r\n")
		return
	}
	s.write("\r\nexited\r\n")
}

// isExpectedExitError filters the read errors every PTY teardown produces.
func isExpectedExitError(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "input/output error") ||
		strings.Contains(msg, "file already closed") ||
		strings.Contains(msg, "EOF")
}

func (s *ptySession) reap() {
	if s == nil || s.cmd == nil {
		return
	}
	s.waitOnce.Do(func() {
		go func() {
			_ = s.cmd.Wait()
		}()
	})
}

func (s *ptySession) stateLabel() string {
	if s == nil {
		return ""
	}
	if s.closed {
		return "exited"
	}
	return "running"
}

func (s *ptySession) writeKey(msg tea.KeyMsg) error {
	if s == nil || s.closed || s.file == nil {
		return nil
	}
	data, ok := keyBytes(msg)
	if !ok {
		return nil
	}
	_, err := s.file.Write(data)
	return err
}

func keyBytes(msg tea.KeyMsg) ([]byte, bool) {
	key := tea.Key(msg)
	switch key.Type {
	case tea.KeyRunes:
		var buf bytes.Buffer
		for _, r := range key.Runes {
			buf.WriteRune(r)
		}
		return buf.Bytes(), true
	case tea.KeySpace:
		return []byte(" "), true
	case tea.KeyEnter:
		return []byte("\r"), true
	case tea.KeyTab:
		return []byte("\t"), true
	case tea.KeyBackspace:
		return []byte{0x7f}, true
	case tea.KeyEsc:
		return []byte{0x1b}, true
	case tea.KeyUp:
		return []byte("\x1b[A"), true
	case tea.KeyDown:
		return []byte("\x1b[B"), true
	case tea.KeyRight:
		return []byte("\x1b[C"), true
	case tea.KeyLeft:
		return []byte("\x1b[D"), true
	case tea.KeyCtrlA, tea.KeyCtrlB, tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlE, tea.KeyCtrlF, tea.KeyCtrlG, tea.KeyCtrlH, tea.KeyCtrlJ, tea.KeyCtrlK, tea.KeyCtrlL, tea.KeyCtrlN, tea.KeyCtrlP, tea.KeyCtrlQ, tea.KeyCtrlR, tea.KeyCtrlS, tea.KeyCtrlT, tea.KeyCtrlU, tea.KeyCtrlV, tea.KeyCtrlW, tea.KeyCtrlX, tea.KeyCtrlY, tea.KeyCtrlZ:
		return []byte{byte(key.Type)}, true
	default:
		return nil, false
	}
}
