package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

// liveSession mirrors ~/.claude/sessions/<pid>.json written by Claude Code.
type liveSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	StartedAt int64  `json:"startedAt"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updatedAt"`
	Name      string `json:"name"`
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil || syscall.Kill(pid, 0) == syscall.EPERM
}

// scanLiveSessions reads live session metadata files. Files whose process no
// longer exists are demoted to done rather than trusted.
func scanLiveSessions(dir string, alive func(int) bool) (map[string]schema.SessionSnapshot, []schema.WarningSnapshot) {
	sessions := map[string]schema.SessionSnapshot{}
	var warnings []schema.WarningSnapshot
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: dir, Message: err.Error()})
		}
		return sessions, warnings
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		payload, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: path, Message: err.Error()})
			continue
		}
		var live liveSession
		if err := json.Unmarshal(payload, &live); err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: path, Message: "invalid session file: " + err.Error()})
			continue
		}
		if live.SessionID == "" {
			continue
		}
		status := schema.SessionUnknown
		switch strings.ToLower(live.Status) {
		case "busy":
			status = schema.SessionBusy
		case "idle":
			status = schema.SessionIdle
		}
		if !alive(live.PID) {
			status = schema.SessionDone
		}
		sessions[live.SessionID] = schema.SessionSnapshot{
			ID:          live.SessionID,
			Provider:    ProviderName,
			Title:       live.Name,
			CWD:         live.CWD,
			Status:      status,
			PID:         live.PID,
			Resumable:   true,
			SourcePath:  path,
			StartedAt:   time.UnixMilli(live.StartedAt),
			LastUpdated: time.UnixMilli(live.UpdatedAt),
		}
	}
	return sessions, warnings
}
