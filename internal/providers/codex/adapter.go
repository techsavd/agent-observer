// Package codex observes OpenAI Codex CLI local session state.
package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/tail"
)

const (
	ProviderName = "codex"

	rolloutTailBytes int64 = 256 << 10
	defaultMaxAge          = 48 * time.Hour
	// liveWindow bounds how long event-derived busy/idle status is trusted;
	// beyond it a session with no writes is shown as done.
	liveWindow = 15 * time.Minute
)

type Config struct {
	CodexDir string
}

type Adapter struct {
	codexDir    string
	sessionsDir string
	maxAge      time.Duration
	tailer      *tail.Tailer
	states      map[string]*rolloutState // keyed by rollout path
	titles      map[string]string        // session id -> thread name
	titleTailer *tail.Tailer
	now         func() time.Time
}

type rolloutState struct {
	session schema.SessionSnapshot
	tokens  schema.TokenUsage
	// lastTaskEvent holds "started" or "complete" from the newest task event.
	lastTaskEvent string
}

func New(cfg Config) *Adapter {
	codexDir := cfg.CodexDir
	if codexDir == "" {
		home, _ := os.UserHomeDir()
		codexDir = filepath.Join(home, ".codex")
	}
	return &Adapter{
		codexDir:    codexDir,
		sessionsDir: filepath.Join(codexDir, "sessions"),
		maxAge:      defaultMaxAge,
		tailer:      tail.New(),
		states:      map[string]*rolloutState{},
		titles:      map[string]string{},
		titleTailer: tail.New(),
		now:         time.Now,
	}
}

func (a *Adapter) Name() string { return ProviderName }

func (a *Adapter) Available() bool {
	if _, err := os.Stat(a.codexDir); err == nil {
		return true
	}
	_, err := exec.LookPath("codex")
	return err == nil
}

func (a *Adapter) WatchPaths() []string {
	paths := []string{a.sessionsDir}
	// The dated leaf directories are where new rollouts appear.
	if dir := a.currentDateDir(); dir != "" {
		paths = append(paths, dir)
	}
	return paths
}

func (a *Adapter) currentDateDir() string {
	now := a.now()
	return filepath.Join(a.sessionsDir,
		strconv.Itoa(now.Year()),
		twoDigit(int(now.Month())),
		twoDigit(now.Day()))
}

func twoDigit(v int) string {
	if v < 10 {
		return "0" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

func (a *Adapter) Snapshot(ctx context.Context) source.ProviderSnapshot {
	start := a.now()
	sessions := map[string]schema.SessionSnapshot{}
	var warnings []schema.WarningSnapshot
	var stats schema.ScanStats
	a.loadTitles()
	seen := map[string]bool{}
	cutoff := a.now().Add(-a.maxAge)
	for _, path := range a.recentRollouts(cutoff, &warnings) {
		if err := ctx.Err(); err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: path, Message: err.Error()})
			break
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		seen[path] = true
		state, known := a.states[path]
		if !known {
			state = &rolloutState{session: schema.SessionSnapshot{
				ID:        sessionIDFromFilename(path),
				Provider:  ProviderName,
				Resumable: true,
				StartedAt: info.ModTime(),
			}}
			a.states[path] = state
		}
		var lines [][]byte
		if known {
			lines, err = a.tailer.Lines(path)
		} else {
			lines, err = a.tailer.TailFrom(path, rolloutTailBytes)
		}
		if err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: path, Message: err.Error()})
		}
		if len(lines) > 0 {
			stats.FilesScanned++
			foldRolloutLines(state, lines)
		} else {
			stats.CacheHits++
		}
		state.session.SourcePath = path
		state.session.LastUpdated = info.ModTime()
		state.session.Status = a.deriveStatus(state, info.ModTime())
		if title, ok := a.titles[state.session.ID]; ok && title != "" {
			state.session.Title = title
		}
		if state.tokens != (schema.TokenUsage{}) {
			tokens := state.tokens
			state.session.Tokens = &tokens
		}
		sessions[state.session.ID] = state.session
	}
	a.tailer.Prune(seen)
	for path := range a.states {
		if !seen[path] {
			delete(a.states, path)
		}
	}
	stats.LastDuration = a.now().Sub(start)
	cliPath, _ := exec.LookPath("codex")
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

// recentRollouts walks sessions/YYYY/MM/DD pruning date directories older
// than the cutoff without statting their contents.
func (a *Adapter) recentRollouts(cutoff time.Time, warnings *[]schema.WarningSnapshot) []string {
	var paths []string
	years, err := os.ReadDir(a.sessionsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			*warnings = append(*warnings, schema.WarningSnapshot{SourcePath: a.sessionsDir, Message: err.Error()})
		}
		return nil
	}
	cutoffDate := cutoff.Truncate(24 * time.Hour)
	for _, year := range years {
		if !year.IsDir() {
			continue
		}
		yearDir := filepath.Join(a.sessionsDir, year.Name())
		months, err := os.ReadDir(yearDir)
		if err != nil {
			continue
		}
		for _, month := range months {
			if !month.IsDir() {
				continue
			}
			monthDir := filepath.Join(yearDir, month.Name())
			days, err := os.ReadDir(monthDir)
			if err != nil {
				continue
			}
			for _, day := range days {
				if !day.IsDir() {
					continue
				}
				if date, err := time.Parse("2006/01/02", year.Name()+"/"+month.Name()+"/"+day.Name()); err == nil {
					if date.Before(cutoffDate) {
						continue
					}
				}
				dayDir := filepath.Join(monthDir, day.Name())
				files, err := os.ReadDir(dayDir)
				if err != nil {
					continue
				}
				for _, file := range files {
					name := file.Name()
					if file.IsDir() || !strings.HasPrefix(name, "rollout-") || filepath.Ext(name) != ".jsonl" {
						continue
					}
					paths = append(paths, filepath.Join(dayDir, name))
				}
			}
		}
	}
	return paths
}

func (a *Adapter) deriveStatus(state *rolloutState, modTime time.Time) schema.SessionStatus {
	if a.now().Sub(modTime) > liveWindow {
		return schema.SessionDone
	}
	switch state.lastTaskEvent {
	case "started":
		return schema.SessionBusy
	case "complete":
		return schema.SessionIdle
	default:
		return schema.SessionIdle
	}
}

// sessionIDFromFilename extracts the uuid suffix of
// rollout-<iso-timestamp>-<uuid>.jsonl; session_meta may be outside the
// tailed window, so the filename is the reliable id source.
func sessionIDFromFilename(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	stem = strings.TrimPrefix(stem, "rollout-")
	if len(stem) > 36 {
		return stem[len(stem)-36:]
	}
	return stem
}

// rolloutLine is the envelope of one rollout event.
type rolloutLine struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func foldRolloutLines(state *rolloutState, lines [][]byte) {
	for _, raw := range lines {
		var line rolloutLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		switch line.Type {
		case "session_meta":
			var meta struct {
				SessionID string `json:"session_id"`
				CWD       string `json:"cwd"`
			}
			if err := json.Unmarshal(line.Payload, &meta); err == nil {
				if meta.SessionID != "" {
					state.session.ID = meta.SessionID
				}
				if meta.CWD != "" {
					state.session.CWD = meta.CWD
				}
				if !line.Timestamp.IsZero() {
					state.session.StartedAt = line.Timestamp
				}
			}
		case "turn_context":
			var turn struct {
				CWD   string `json:"cwd"`
				Model string `json:"model"`
			}
			if err := json.Unmarshal(line.Payload, &turn); err == nil {
				if turn.CWD != "" {
					state.session.CWD = turn.CWD
				}
				if turn.Model != "" {
					state.session.Model = turn.Model
				}
			}
		case "event_msg":
			foldEventMsg(state, line.Payload)
		case "response_item":
			var item struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.Unmarshal(line.Payload, &item); err == nil && item.Type == "message" {
				state.session.Turns++
				for i := len(item.Content) - 1; i >= 0; i-- {
					if text := strings.TrimSpace(item.Content[i].Text); text != "" {
						state.session.LastText = clipText(text)
						break
					}
				}
			}
		}
	}
}

func foldEventMsg(state *rolloutState, payload json.RawMessage) {
	var event struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Info    struct {
			TotalTokenUsage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return
	}
	switch event.Type {
	case "task_started":
		state.lastTaskEvent = "started"
	case "task_complete":
		state.lastTaskEvent = "complete"
	case "user_message", "agent_message":
		if text := strings.TrimSpace(event.Message); text != "" {
			state.session.LastText = clipText(text)
		}
	case "token_count":
		// token_count reports running totals, so replace instead of adding.
		state.tokens.Input = event.Info.TotalTokenUsage.InputTokens
		state.tokens.Output = event.Info.TotalTokenUsage.OutputTokens
	}
}

const maxLastText = 200

func clipText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxLastText {
		text = text[:maxLastText] + "…"
	}
	return text
}

// loadTitles incrementally reads session_index.jsonl mapping ids to names.
func (a *Adapter) loadTitles() {
	path := filepath.Join(a.codexDir, "session_index.jsonl")
	lines, err := a.titleTailer.Lines(path)
	if err != nil {
		return
	}
	for _, raw := range lines {
		var entry struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if err := json.Unmarshal(raw, &entry); err == nil && entry.ID != "" {
			a.titles[entry.ID] = entry.ThreadName
		}
	}
}
