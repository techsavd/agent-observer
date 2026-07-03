// Package claude adapts local Claude Code state to the source.Adapter contract.
package claude

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	claudescan "github.com/techsavd/agent-observer/internal/claude"
)

const ProviderName = "claude"

type Adapter struct {
	claudeDir   string
	sessionsDir string
	scanner     *claudescan.Scanner
	transcripts *transcriptScanner
	alive       func(int) bool
}

type Config struct {
	ClaudeDir   string
	TasksDir    string
	TeamsDir    string
	MaxFileSize int64
}

func New(cfg Config) *Adapter {
	claudeDir := cfg.ClaudeDir
	if claudeDir == "" {
		home, _ := os.UserHomeDir()
		claudeDir = filepath.Join(home, ".claude")
	}
	tasksDir := cfg.TasksDir
	if tasksDir == "" {
		tasksDir = filepath.Join(claudeDir, "tasks")
	}
	teamsDir := cfg.TeamsDir
	if teamsDir == "" {
		teamsDir = filepath.Join(claudeDir, "teams")
	}
	return &Adapter{
		claudeDir:   claudeDir,
		sessionsDir: filepath.Join(claudeDir, "sessions"),
		scanner:     claudescan.NewScanner(tasksDir, teamsDir, cfg.MaxFileSize),
		transcripts: newTranscriptScanner(filepath.Join(claudeDir, "projects")),
		alive:       pidAlive,
	}
}

func (a *Adapter) Name() string { return ProviderName }

func (a *Adapter) Available() bool {
	if _, err := os.Stat(a.claudeDir); err == nil {
		return true
	}
	_, err := exec.LookPath("claude")
	return err == nil
}

func (a *Adapter) WatchPaths() []string {
	return []string{a.scanner.TasksDir, a.scanner.TeamsDir, a.sessionsDir, a.transcripts.projectsDir}
}

func (a *Adapter) Snapshot(ctx context.Context) source.ProviderSnapshot {
	world := a.scanner.ScanContext(ctx)
	live, liveWarnings := scanLiveSessions(a.sessionsDir, a.alive)
	derived, transcriptWarnings, transcriptStats := a.transcripts.scan()
	sessions := mergeSessions(live, derived)
	warnings := append(world.Warnings, liveWarnings...)
	warnings = append(warnings, transcriptWarnings...)
	stats := world.Stats
	stats.FilesScanned += transcriptStats.FilesScanned
	stats.CacheHits += transcriptStats.CacheHits
	cliPath, _ := exec.LookPath("claude")
	return source.ProviderSnapshot{
		Provider: ProviderName,
		Info: schema.ProviderInfo{
			Name:      ProviderName,
			Available: a.Available(),
			CLIPath:   cliPath,
		},
		Sessions: sessions,
		Tasks:    world.Tasks,
		Batches:  world.Batches,
		Warnings: warnings,
		Stats:    stats,
	}
}

// mergeSessions overlays live process metadata (authoritative status, pid,
// title) onto transcript-derived detail (model, turns, last text, tokens).
func mergeSessions(live, derived map[string]schema.SessionSnapshot) map[string]schema.SessionSnapshot {
	sessions := make(map[string]schema.SessionSnapshot, len(derived)+len(live))
	for id, session := range derived {
		// A transcript without a live process is a finished conversation.
		session.Status = schema.SessionDone
		sessions[id] = session
	}
	for id, session := range live {
		if existing, ok := sessions[id]; ok {
			existing.Status = session.Status
			existing.PID = session.PID
			if session.Title != "" {
				existing.Title = session.Title
			}
			if session.CWD != "" {
				existing.CWD = session.CWD
			}
			if session.LastUpdated.After(existing.LastUpdated) {
				existing.LastUpdated = session.LastUpdated
			}
			if !session.StartedAt.IsZero() {
				existing.StartedAt = session.StartedAt
			}
			sessions[id] = existing
			continue
		}
		sessions[id] = session
	}
	return sessions
}
