package schema

import "time"

type TaskStatus string
type AgentRole string
type SessionStatus string

const CurrentSchemaVersion = "v2"

const (
	StatusRunning   TaskStatus = "running"
	StatusWaiting   TaskStatus = "waiting"
	StatusBlocked   TaskStatus = "blocked"
	StatusCompleted TaskStatus = "completed"
	StatusErrored   TaskStatus = "errored"
	StatusUnknown   TaskStatus = "unknown"

	SessionBusy    SessionStatus = "busy"
	SessionIdle    SessionStatus = "idle"
	SessionDone    SessionStatus = "done"
	SessionUnknown SessionStatus = "unknown"

	RoleLead     AgentRole = "Lead"
	RoleCoding   AgentRole = "Coding"
	RoleReviewer AgentRole = "Reviewer"
	RoleQA       AgentRole = "QA"
)

type WorldSnapshot struct {
	SchemaVersion string                     `json:"schema_version"`
	Providers     map[string]ProviderInfo    `json:"providers"`
	Sessions      map[string]SessionSnapshot `json:"sessions"`
	Tasks         map[string]TaskSnapshot    `json:"tasks"`
	Batches       map[string]BatchSnapshot   `json:"batches"`
	Warnings      []WarningSnapshot          `json:"warnings"`
	Stats         ScanStats                  `json:"stats"`
}

type ProviderInfo struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	CLIPath   string   `json:"cli_path,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

type SessionSnapshot struct {
	ID          string        `json:"id"`
	Provider    string        `json:"provider"`
	Title       string        `json:"title,omitempty"`
	CWD         string        `json:"cwd,omitempty"`
	Status      SessionStatus `json:"status"`
	Model       string        `json:"model,omitempty"`
	LastText    string        `json:"last_text,omitempty"`
	Turns       int           `json:"turns,omitempty"`
	Tokens      *TokenUsage   `json:"tokens,omitempty"`
	PID         int           `json:"pid,omitempty"`
	Resumable   bool          `json:"resumable,omitempty"`
	SourcePath  string        `json:"source_path,omitempty"`
	StartedAt   time.Time     `json:"started_at,omitempty"`
	LastUpdated time.Time     `json:"last_updated"`
}

type TokenUsage struct {
	Input  int `json:"input,omitempty"`
	Output int `json:"output,omitempty"`
}

type BatchSnapshot struct {
	BatchID       string      `json:"batch_id"`
	Provider      string      `json:"provider,omitempty"`
	TaskIDs       []string    `json:"task_ids"`
	Counts        BatchCounts `json:"counts"`
	LastUpdated   time.Time   `json:"last_updated"`
	HighWatermark *int        `json:"high_watermark,omitempty"`
	HasLock       *bool       `json:"has_lock,omitempty"`
}

type BatchCounts struct {
	Running   int `json:"running"`
	Waiting   int `json:"waiting"`
	Blocked   int `json:"blocked"`
	Completed int `json:"completed"`
	Errored   int `json:"errored"`
	Unknown   int `json:"unknown"`
}

type TaskSnapshot struct {
	ID          string       `json:"id"`
	BatchID     string       `json:"batch_id"`
	Provider    string       `json:"provider,omitempty"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	ActiveForm  string       `json:"active_form"`
	Status      TaskStatus   `json:"status"`
	Role        AgentRole    `json:"role"`
	ActiveFiles []ActiveFile `json:"active_files,omitempty"`
	SourcePath  string       `json:"source_path"`
	LastUpdated time.Time    `json:"last_updated"`
}

type ActiveFile struct {
	Path string `json:"path"`
}

type WarningSnapshot struct {
	SourcePath string `json:"source_path"`
	Message    string `json:"message"`
}

type ScanStats struct {
	// LastTrigger records what caused the latest scan: watch, poll, or manual.
	LastTrigger     string        `json:"last_trigger,omitempty"`
	FilesScanned    int           `json:"files_scanned"`
	CacheHits       int           `json:"cache_hits"`
	SkippedOversize int           `json:"skipped_oversize"`
	SkippedSymlinks int           `json:"skipped_symlinks"`
	PartialRetries  int           `json:"partial_retries"`
	Warnings        int           `json:"warnings"`
	LastDuration    time.Duration `json:"last_duration"`
}
