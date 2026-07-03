package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/tail"
)

const (
	// transcriptTailBytes bounds how much history is parsed the first time a
	// transcript is seen, so cold starts skip old conversation bulk.
	transcriptTailBytes int64 = 256 << 10
	defaultMaxAge             = 48 * time.Hour
)

// transcriptEntry is the subset of a Claude Code transcript line we consume.
type transcriptEntry struct {
	Type      string    `json:"type"`
	SessionID string    `json:"sessionId"`
	Timestamp time.Time `json:"timestamp"`
	CWD       string    `json:"cwd"`
	Message   struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// transcriptState accumulates per-session facts across incremental reads.
type transcriptState struct {
	session schema.SessionSnapshot
	tokens  schema.TokenUsage
}

type transcriptScanner struct {
	projectsDir string
	maxAge      time.Duration
	tailer      *tail.Tailer
	states      map[string]*transcriptState // keyed by transcript path
}

func newTranscriptScanner(projectsDir string) *transcriptScanner {
	return &transcriptScanner{
		projectsDir: projectsDir,
		maxAge:      defaultMaxAge,
		tailer:      tail.New(),
		states:      map[string]*transcriptState{},
	}
}

// scan tails recent transcripts and returns sessions derived from them.
func (t *transcriptScanner) scan() (map[string]schema.SessionSnapshot, []schema.WarningSnapshot, schema.ScanStats) {
	sessions := map[string]schema.SessionSnapshot{}
	var warnings []schema.WarningSnapshot
	var stats schema.ScanStats
	seen := map[string]bool{}
	defer func() {
		t.tailer.Prune(seen)
		for path := range t.states {
			if !seen[path] {
				delete(t.states, path)
			}
		}
	}()
	projects, err := os.ReadDir(t.projectsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: t.projectsDir, Message: err.Error()})
		}
		return sessions, warnings, stats
	}
	cutoff := time.Now().Add(-t.maxAge)
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(t.projectsDir, project.Name())
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: projectDir, Message: err.Error()})
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				continue
			}
			path := filepath.Join(projectDir, entry.Name())
			info, err := entry.Info()
			if err != nil || info.ModTime().Before(cutoff) {
				continue
			}
			seen[path] = true
			state, known := t.states[path]
			if !known {
				state = &transcriptState{session: schema.SessionSnapshot{
					ID:        strings.TrimSuffix(entry.Name(), ".jsonl"),
					Provider:  ProviderName,
					Resumable: true,
					StartedAt: info.ModTime(),
				}}
				t.states[path] = state
			}
			var lines [][]byte
			if known {
				lines, err = t.tailer.Lines(path)
			} else {
				lines, err = t.tailer.TailFrom(path, transcriptTailBytes)
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
			if state.tokens != (schema.TokenUsage{}) {
				tokens := state.tokens
				state.session.Tokens = &tokens
			}
			sessions[state.session.ID] = state.session
		}
	}
	return sessions, warnings, stats
}

func foldTranscriptLines(state *transcriptState, lines [][]byte) {
	for _, line := range lines {
		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "user", "assistant":
		default:
			continue
		}
		state.session.Turns++
		if entry.CWD != "" {
			state.session.CWD = entry.CWD
		}
		if !entry.Timestamp.IsZero() && state.session.StartedAt.After(entry.Timestamp) {
			state.session.StartedAt = entry.Timestamp
		}
		if text := contentText(entry.Message.Content); text != "" {
			state.session.LastText = text
		}
		if entry.Type == "assistant" {
			if entry.Message.Model != "" {
				state.session.Model = entry.Message.Model
			}
			state.tokens.Input += entry.Message.Usage.InputTokens
			state.tokens.Output += entry.Message.Usage.OutputTokens
		}
	}
}

const maxLastText = 200

// contentText extracts displayable text from a message content value, which
// is either a plain string or an array of typed blocks.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return clipText(text)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Type == "text" && strings.TrimSpace(blocks[i].Text) != "" {
			return clipText(blocks[i].Text)
		}
	}
	return ""
}

func clipText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxLastText {
		text = text[:maxLastText] + "…"
	}
	return text
}
