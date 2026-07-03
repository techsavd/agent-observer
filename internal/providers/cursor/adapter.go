// Package cursor observes Cursor CLI agent transcripts.
package cursor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/tail"
)

const (
	ProviderName = "cursor"

	transcriptTailBytes int64 = 256 << 10
	defaultMaxAge             = 48 * time.Hour
	// Cursor transcripts carry no explicit status; recency stands in for it.
	busyWithin = 20 * time.Second
	idleWithin = 10 * time.Minute
)

type Config struct {
	CursorDir string
}

type Adapter struct {
	cursorDir   string
	projectsDir string
	maxAge      time.Duration
	tailer      *tail.Tailer
	states      map[string]*transcriptState
	now         func() time.Time
}

type transcriptState struct {
	session schema.SessionSnapshot
}

func New(cfg Config) *Adapter {
	cursorDir := cfg.CursorDir
	if cursorDir == "" {
		home, _ := os.UserHomeDir()
		cursorDir = filepath.Join(home, ".cursor")
	}
	return &Adapter{
		cursorDir:   cursorDir,
		projectsDir: filepath.Join(cursorDir, "projects"),
		maxAge:      defaultMaxAge,
		tailer:      tail.New(),
		states:      map[string]*transcriptState{},
		now:         time.Now,
	}
}

func (a *Adapter) Name() string { return ProviderName }

func (a *Adapter) Available() bool {
	if _, err := os.Stat(a.projectsDir); err == nil {
		return true
	}
	_, err := exec.LookPath("cursor-agent")
	return err == nil
}

func (a *Adapter) WatchPaths() []string {
	return []string{a.projectsDir}
}

func (a *Adapter) Snapshot(ctx context.Context) source.ProviderSnapshot {
	start := a.now()
	sessions := map[string]schema.SessionSnapshot{}
	var warnings []schema.WarningSnapshot
	var stats schema.ScanStats
	seen := map[string]bool{}
	cutoff := a.now().Add(-a.maxAge)
	projects, err := os.ReadDir(a.projectsDir)
	if err != nil && !os.IsNotExist(err) {
		warnings = append(warnings, schema.WarningSnapshot{SourcePath: a.projectsDir, Message: err.Error()})
	}
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			break
		}
		if !project.IsDir() {
			continue
		}
		transcriptsDir := filepath.Join(a.projectsDir, project.Name(), "agent-transcripts")
		sessionDirs, err := os.ReadDir(transcriptsDir)
		if err != nil {
			continue
		}
		for _, sessionDir := range sessionDirs {
			if !sessionDir.IsDir() {
				continue
			}
			dir := filepath.Join(transcriptsDir, sessionDir.Name())
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, file := range files {
				if file.IsDir() || filepath.Ext(file.Name()) != ".jsonl" {
					continue
				}
				path := filepath.Join(dir, file.Name())
				info, err := file.Info()
				if err != nil || info.ModTime().Before(cutoff) {
					continue
				}
				seen[path] = true
				state, known := a.states[path]
				if !known {
					state = &transcriptState{session: schema.SessionSnapshot{
						ID:        sessionDir.Name(),
						Provider:  ProviderName,
						Title:     projectTitle(project.Name()),
						CWD:       decodeProjectPath(project.Name()),
						StartedAt: info.ModTime(),
					}}
					a.states[path] = state
				}
				var lines [][]byte
				if known {
					lines, err = a.tailer.Lines(path)
				} else {
					lines, err = a.tailer.TailFrom(path, transcriptTailBytes)
				}
				if err != nil {
					warnings = append(warnings, schema.WarningSnapshot{SourcePath: path, Message: err.Error()})
				}
				if len(lines) > 0 {
					stats.FilesScanned++
					foldTranscriptLines(state, lines)
				} else {
					stats.CacheHits++
				}
				state.session.SourcePath = path
				state.session.LastUpdated = info.ModTime()
				state.session.Status = a.statusFromRecency(info.ModTime())
				sessions[state.session.ID] = state.session
			}
		}
	}
	a.tailer.Prune(seen)
	for path := range a.states {
		if !seen[path] {
			delete(a.states, path)
		}
	}
	stats.LastDuration = a.now().Sub(start)
	cliPath, _ := exec.LookPath("cursor-agent")
	return source.ProviderSnapshot{
		Provider: ProviderName,
		Info: schema.ProviderInfo{
			Name:      ProviderName,
			Available: a.Available(),
			CLIPath:   cliPath,
		},
		Sessions: sessions,
		Warnings: warnings,
		Stats:    stats,
	}
}

func (a *Adapter) statusFromRecency(modTime time.Time) schema.SessionStatus {
	switch since := a.now().Sub(modTime); {
	case since <= busyWithin:
		return schema.SessionBusy
	case since <= idleWithin:
		return schema.SessionIdle
	default:
		return schema.SessionDone
	}
}

type cursorLine struct {
	Role    string `json:"role"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func foldTranscriptLines(state *transcriptState, lines [][]byte) {
	for _, raw := range lines {
		var line cursorLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		if line.Role != "user" && line.Role != "assistant" {
			continue
		}
		state.session.Turns++
		for i := len(line.Message.Content) - 1; i >= 0; i-- {
			if text := strings.TrimSpace(line.Message.Content[i].Text); text != "" {
				state.session.LastText = clipText(text)
				break
			}
		}
	}
}

// decodeProjectPath reverses Cursor's dash-encoded absolute paths
// (e.g. "-Users-me-code-app" -> "/Users/me/code/app"). Names that do not
// look encoded are returned empty; the project title still identifies them.
func decodeProjectPath(name string) string {
	if !strings.HasPrefix(name, "-") {
		return ""
	}
	return strings.ReplaceAll(name, "-", "/")
}

func projectTitle(name string) string {
	trimmed := strings.Trim(name, "-")
	parts := strings.Split(trimmed, "-")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

const maxLastText = 200

func clipText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxLastText {
		text = text[:maxLastText] + "…"
	}
	return text
}
