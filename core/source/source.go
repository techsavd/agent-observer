package source

import (
	"context"
	"time"
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

type Warning struct {
	SourcePath string
	Message    string
}

type Stats struct {
	FilesScanned    int
	CacheHits       int
	SkippedOversize int
	PartialRetries  int
}

type Snapshot struct {
	Records  []Record
	Warnings []Warning
	Stats    Stats
}

type Adapter interface {
	Name() string
	Snapshot(ctx context.Context) Snapshot
}
