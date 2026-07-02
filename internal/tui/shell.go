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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

type shellSession struct {
	cmd      *exec.Cmd
	file     *os.File
	cwd      string
	shell    string
	screen   *screen
	raw      []string
	partial  string
	closed   bool
	err      error
	waitOnce sync.Once
}

var ansiPattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\a]*(?:\a|\x1b\\)|[()][0-9A-Za-z]|.)`)

type shellStartedMsg struct {
	session *shellSession
	err     error
}

type shellOutputMsg struct {
	text string
	err  error
}

func startShell(cwd string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		shellPath := os.Getenv("SHELL")
		if strings.TrimSpace(shellPath) == "" {
			shellPath = "/bin/sh"
		}
		cmd := exec.Command(shellPath)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "TERM=xterm-256color", "AGENT_OBSERVER_SHELL=1")
		file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(max(20, cols)), Rows: uint16(max(5, rows))})
		if err != nil {
			return shellStartedMsg{err: err}
		}
		session := &shellSession{cmd: cmd, file: file, cwd: cwd, shell: shellPath, screen: newScreen(cols, rows)}
		session.write(fmt.Sprintf("local PTY started: %s\r\nctrl+o leaves shell focus; ctrl+d exits shell\r\n", shellPath))
		return shellStartedMsg{session: session}
	}
}

func readShell(s *shellSession) tea.Cmd {
	if s == nil || s.closed || s.file == nil {
		return nil
	}
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := s.file.Read(buf)
		if n > 0 {
			return shellOutputMsg{text: string(buf[:n])}
		}
		return shellOutputMsg{err: err}
	}
}

func (s *shellSession) write(text string) {
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

func (s *shellSession) lines(limit int) []string {
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

func (s *shellSession) resize(cols, rows int) {
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

func (s *shellSession) close() {
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

func (s *shellSession) markClosed(err error) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.file != nil {
		_ = s.file.Close()
	}
	s.reap()
	if err != nil {
		s.err = err
		s.write("\r\nshell exited: " + err.Error() + "\r\n")
		return
	}
	s.write("\r\nshell exited\r\n")
}

func (s *shellSession) reap() {
	if s == nil || s.cmd == nil {
		return
	}
	s.waitOnce.Do(func() {
		go func() {
			_ = s.cmd.Wait()
		}()
	})
}

func (s *shellSession) writeKey(msg tea.KeyMsg) error {
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
