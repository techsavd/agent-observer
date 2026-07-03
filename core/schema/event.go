package schema

import "time"

type EventType string

const (
	EventTaskObserved      EventType = "task.observed"
	EventTaskStatusChanged EventType = "task.status_changed"
	EventBatchObserved     EventType = "batch.observed"
	EventBatchLockObserved EventType = "batch.lock_observed"
	EventBatchWatermark    EventType = "batch.high_watermark_observed"
	EventFilesTouched      EventType = "task.files_touched"
	EventWarningObserved   EventType = "warning.observed"
)

type ObserverEvent struct {
	Type       EventType         `json:"type"`
	Source     string            `json:"source"`
	SourcePath string            `json:"source_path,omitempty"`
	BatchID    string            `json:"batch_id,omitempty"`
	TaskID     string            `json:"task_id,omitempty"`
	Status     TaskStatus        `json:"status,omitempty"`
	Role       AgentRole         `json:"role,omitempty"`
	Files      []ActiveFile      `json:"files,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	ObservedAt time.Time         `json:"observed_at"`
}
