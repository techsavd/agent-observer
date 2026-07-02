package manifest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/tail"
)

const (
	fileTailBytes int64 = 256 << 10
	defaultMaxAge       = 48 * time.Hour

	defaultBusyWithin = 30 * time.Second
	defaultIdleWithin = 10 * time.Minute
)

// Adapter is a generic tail-and-extract observer driven by one Manifest.
type Adapter struct {
	manifest Manifest
	maxAge   time.Duration
	tailer   *tail.Tailer
	states   map[string]*sessionState
	now      func() time.Time
}

type sessionState struct {
	session schema.SessionSnapshot
}

func NewAdapter(m Manifest) *Adapter {
	return &Adapter{
		manifest: m,
		maxAge:   defaultMaxAge,
		tailer:   tail.New(),
		states:   map[string]*sessionState{},
		now:      time.Now,
	}
}

func (a *Adapter) Name() string { return a.manifest.Name }

func (a *Adapter) Manifest() Manifest { return a.manifest }

func (a *Adapter) Available() bool {
	for _, glob := range a.manifest.Watch.Globs {
		if dir := staticPrefix(expandHome(glob)); dir != "" {
			if _, err := os.Stat(dir); err == nil {
				return true
			}
		}
	}
	return false
}

func (a *Adapter) WatchPaths() []string {
	var paths []string
	for _, glob := range a.manifest.Watch.Globs {
		if dir := staticPrefix(expandHome(glob)); dir != "" {
			paths = append(paths, dir)
		}
	}
	return paths
}

func (a *Adapter) Snapshot(ctx context.Context) source.ProviderSnapshot {
	start := a.now()
	sessions := map[string]schema.SessionSnapshot{}
	var warnings []schema.WarningSnapshot
	var stats schema.ScanStats
	seen := map[string]bool{}
	cutoff := a.now().Add(-a.maxAge)
	for _, glob := range a.manifest.Watch.Globs {
		matches, err := filepath.Glob(expandHome(glob))
		if err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: glob, Message: err.Error()})
			continue
		}
		for _, path := range matches {
			if err := ctx.Err(); err != nil {
				break
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() || info.ModTime().Before(cutoff) {
				continue
			}
			seen[path] = true
			state, known := a.states[path]
			if !known {
				state = &sessionState{session: schema.SessionSnapshot{
					ID:        a.sessionID(path),
					Provider:  a.manifest.Name,
					Resumable: len(a.manifest.Commands.Resume) > 0,
					StartedAt: info.ModTime(),
				}}
				a.states[path] = state
			}
			var lines [][]byte
			if known {
				lines, err = a.tailer.Lines(path)
			} else {
				lines, err = a.tailer.TailFrom(path, fileTailBytes)
			}
			if err != nil {
				warnings = append(warnings, schema.WarningSnapshot{SourcePath: path, Message: err.Error()})
			}
			if len(lines) > 0 {
				stats.FilesScanned++
				a.foldLines(state, lines)
			} else {
				stats.CacheHits++
			}
			state.session.SourcePath = path
			state.session.LastUpdated = info.ModTime()
			state.session.Status = a.statusFromRecency(info.ModTime())
			sessions[state.session.ID] = state.session
		}
	}
	a.tailer.Prune(seen)
	for path := range a.states {
		if !seen[path] {
			delete(a.states, path)
		}
	}
	stats.LastDuration = a.now().Sub(start)
	return source.ProviderSnapshot{
		Provider: a.manifest.Name,
		Info: schema.ProviderInfo{
			Name:      a.manifest.Name,
			Available: a.Available(),
		},
		Sessions: sessions,
		Warnings: warnings,
		Stats:    stats,
	}
}

func (a *Adapter) sessionID(path string) string {
	id := a.manifest.Session.ID
	switch {
	case id == "dir_name":
		return filepath.Base(filepath.Dir(path))
	case strings.HasPrefix(id, "field:"):
		// Field-based ids are filled while folding lines; use the stem until
		// the field is first seen.
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	default: // "" or "filename_stem"
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
}

func (a *Adapter) foldLines(state *sessionState, lines [][]byte) {
	idField := strings.TrimPrefix(a.manifest.Session.ID, "field:")
	useIDField := strings.HasPrefix(a.manifest.Session.ID, "field:")
	cwdField := strings.TrimPrefix(a.manifest.Session.CWD, "field:")
	textField := strings.TrimPrefix(a.manifest.Session.Text, "field:")
	for _, raw := range lines {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		state.session.Turns++
		if useIDField {
			if id := extractField(value, idField); id != "" {
				state.session.ID = id
			}
		}
		if a.manifest.Session.CWD != "" {
			if cwd := extractField(value, cwdField); cwd != "" {
				state.session.CWD = cwd
			}
		}
		if a.manifest.Session.Text != "" {
			if text := extractField(value, textField); strings.TrimSpace(text) != "" {
				state.session.LastText = clipText(text)
			}
		}
	}
}

func (a *Adapter) statusFromRecency(modTime time.Time) schema.SessionStatus {
	busy, idle := defaultBusyWithin, defaultIdleWithin
	if v := a.manifest.Session.Status.BusyWithin.Duration; v > 0 {
		busy = v
	}
	if v := a.manifest.Session.Status.IdleWithin.Duration; v > 0 {
		idle = v
	}
	switch since := a.now().Sub(modTime); {
	case since <= busy:
		return schema.SessionBusy
	case since <= idle:
		return schema.SessionIdle
	default:
		return schema.SessionDone
	}
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// staticPrefix returns the longest directory prefix without glob metacharacters.
func staticPrefix(glob string) string {
	parts := strings.Split(glob, string(filepath.Separator))
	var kept []string
	for _, part := range parts {
		if strings.ContainsAny(part, "*?[") {
			break
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return ""
	}
	prefix := strings.Join(kept, string(filepath.Separator))
	if prefix == "" {
		return string(filepath.Separator)
	}
	return prefix
}

const maxLastText = 200

func clipText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxLastText {
		text = text[:maxLastText] + "…"
	}
	return text
}
