package store

import (
	"sync"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

type MemoryStore struct {
	mu       sync.RWMutex
	tasks    map[string]schema.TaskSnapshot
	batches  map[string]schema.BatchSnapshot
	warnings []schema.WarningSnapshot
	stats    schema.ScanStats
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks:   map[string]schema.TaskSnapshot{},
		batches: map[string]schema.BatchSnapshot{},
	}
}

func (s *MemoryStore) Replace(tasks map[string]schema.TaskSnapshot, warnings []schema.WarningSnapshot, stats schema.ScanStats) schema.WorldSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = copyTasks(tasks)
	s.warnings = append([]schema.WarningSnapshot{}, warnings...)
	s.stats = stats
	s.rebuildBatches()
	return s.snapshotLocked()
}

func (s *MemoryStore) Snapshot() schema.WorldSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *MemoryStore) snapshotLocked() schema.WorldSnapshot {
	return schema.WorldSnapshot{
		SchemaVersion: schema.CurrentSchemaVersion,
		Tasks:         copyTasks(s.tasks),
		Batches:       copyBatches(s.batches),
		Warnings:      append([]schema.WarningSnapshot{}, s.warnings...),
		Stats:         s.stats,
	}
}

func (s *MemoryStore) rebuildBatches() {
	batches := map[string]schema.BatchSnapshot{}
	for _, task := range s.tasks {
		batch := batches[task.BatchID]
		batch.BatchID = task.BatchID
		batch.TaskIDs = append(batch.TaskIDs, task.ID)
		if task.LastUpdated.After(batch.LastUpdated) {
			batch.LastUpdated = task.LastUpdated
		}
		addCount(&batch.Counts, task.Status)
		batches[task.BatchID] = batch
	}
	s.batches = batches
}

func copyTasks(in map[string]schema.TaskSnapshot) map[string]schema.TaskSnapshot {
	out := make(map[string]schema.TaskSnapshot, len(in))
	for key, value := range in {
		value.ActiveFiles = append([]schema.ActiveFile{}, value.ActiveFiles...)
		out[key] = value
	}
	return out
}

func copyBatches(in map[string]schema.BatchSnapshot) map[string]schema.BatchSnapshot {
	out := make(map[string]schema.BatchSnapshot, len(in))
	for key, value := range in {
		value.TaskIDs = append([]string{}, value.TaskIDs...)
		out[key] = value
	}
	return out
}

func addCount(counts *schema.BatchCounts, status schema.TaskStatus) {
	switch status {
	case schema.StatusRunning:
		counts.Running++
	case schema.StatusWaiting:
		counts.Waiting++
	case schema.StatusBlocked:
		counts.Blocked++
	case schema.StatusCompleted:
		counts.Completed++
	case schema.StatusErrored:
		counts.Errored++
	default:
		counts.Unknown++
	}
}

func Latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
