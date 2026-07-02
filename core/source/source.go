package source

import (
	"context"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

type Kind string

const (
	KindTaskJSON      Kind = "claude.task_json"
	KindBatchLock     Kind = "claude.batch_lock"
	KindHighWatermark Kind = "claude.high_watermark"
)

type Record struct {
	Kind      Kind
	Source    string
	Path      string
	BatchID   string
	TaskIndex string
	Payload   []byte
	ModTime   time.Time
	Size      int64
}

// ProviderSnapshot is one provider's contribution to the merged world state.
type ProviderSnapshot struct {
	Provider string
	Info     schema.ProviderInfo
	Sessions map[string]schema.SessionSnapshot
	Tasks    map[string]schema.TaskSnapshot
	Batches  map[string]schema.BatchSnapshot
	Warnings []schema.WarningSnapshot
	Stats    schema.ScanStats
}

// Adapter observes one provider's local state. Implementations must be safe
// to call repeatedly; Snapshot should be cheap when nothing changed.
type Adapter interface {
	Name() string
	Available() bool
	Snapshot(ctx context.Context) ProviderSnapshot
	// WatchPaths lists directories whose changes should trigger a rescan.
	WatchPaths() []string
}
