package tui

import (
	"fmt"
	"strconv"

	"github.com/techsavd/agent-observer/core/source"
)

// ActionProvider pairs a provider name with its launch/resume capability.
// run.go builds the list from adapters that implement source.Actor.
type ActionProvider struct {
	Name  string
	Actor source.Actor
}

const maxManagedSessions = 4

// sessionManager owns the managed PTY panes. Only the active one renders
// and receives keys; [ and ] cycle through the rest.
type sessionManager struct {
	byID   map[string]*ptySession
	order  []string
	active int
	nextID int
}

func newSessionManager() *sessionManager {
	return &sessionManager{byID: map[string]*ptySession{}}
}

func (m *sessionManager) newID() string {
	m.nextID++
	return "run-" + strconv.Itoa(m.nextID)
}

// add registers a started session and makes it active. When the pane budget
// is exhausted it evicts the oldest exited pane; live panes are never
// evicted implicitly.
func (m *sessionManager) add(s *ptySession) error {
	if len(m.order) >= maxManagedSessions {
		if !m.evictOldestExited() {
			return fmt.Errorf("all %d panes are running; stop one with x first", maxManagedSessions)
		}
	}
	m.byID[s.id] = s
	m.order = append(m.order, s.id)
	m.active = len(m.order) - 1
	return nil
}

func (m *sessionManager) evictOldestExited() bool {
	for _, id := range m.order {
		if session := m.byID[id]; session != nil && session.closed {
			m.remove(id)
			return true
		}
	}
	return false
}

func (m *sessionManager) remove(id string) {
	session := m.byID[id]
	if session != nil && !session.closed {
		session.close()
	}
	delete(m.byID, id)
	for i, candidate := range m.order {
		if candidate == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			if m.active >= len(m.order) {
				m.active = max(0, len(m.order)-1)
			} else if m.active > i {
				m.active--
			}
			break
		}
	}
}

func (m *sessionManager) get(id string) *ptySession {
	return m.byID[id]
}

func (m *sessionManager) activeSession() *ptySession {
	if len(m.order) == 0 {
		return nil
	}
	return m.byID[m.order[clamp(m.active, 0, len(m.order)-1)]]
}

func (m *sessionManager) cycle(delta int) {
	if len(m.order) == 0 {
		return
	}
	m.active = (m.active + delta + len(m.order)) % len(m.order)
}

func (m *sessionManager) count() int { return len(m.order) }

func (m *sessionManager) position() string {
	if len(m.order) == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", clamp(m.active, 0, len(m.order)-1)+1, len(m.order))
}

// shellSessionByProvider finds an existing pane, used to re-focus the plain
// shell instead of spawning a second one.
func (m *sessionManager) byProvider(provider string) *ptySession {
	for _, id := range m.order {
		if session := m.byID[id]; session != nil && session.provider == provider && !session.closed {
			return session
		}
	}
	return nil
}

func (m *sessionManager) closeAll() {
	for _, session := range m.byID {
		session.close()
	}
}
