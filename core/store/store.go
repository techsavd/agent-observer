package store

import (
	"sync"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

// MemoryStore holds the latest merged world snapshot. Writers replace the
// whole world; readers always get an independent deep copy.
type MemoryStore struct {
	mu    sync.RWMutex
	world schema.WorldSnapshot
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{world: schema.WorldSnapshot{
		SchemaVersion: schema.CurrentSchemaVersion,
		Providers:     map[string]schema.ProviderInfo{},
		Sessions:      map[string]schema.SessionSnapshot{},
		Tasks:         map[string]schema.TaskSnapshot{},
		Batches:       map[string]schema.BatchSnapshot{},
	}}
}

func (s *MemoryStore) Replace(world schema.WorldSnapshot) schema.WorldSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.world = copyWorld(world)
	return copyWorld(s.world)
}

func (s *MemoryStore) Snapshot() schema.WorldSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyWorld(s.world)
}

func copyWorld(in schema.WorldSnapshot) schema.WorldSnapshot {
	out := in
	out.Providers = make(map[string]schema.ProviderInfo, len(in.Providers))
	for key, value := range in.Providers {
		value.Warnings = append([]string{}, value.Warnings...)
		out.Providers[key] = value
	}
	out.Sessions = make(map[string]schema.SessionSnapshot, len(in.Sessions))
	for key, value := range in.Sessions {
		if value.Tokens != nil {
			tokens := *value.Tokens
			value.Tokens = &tokens
		}
		out.Sessions[key] = value
	}
	out.Tasks = make(map[string]schema.TaskSnapshot, len(in.Tasks))
	for key, value := range in.Tasks {
		value.ActiveFiles = append([]schema.ActiveFile{}, value.ActiveFiles...)
		out.Tasks[key] = value
	}
	out.Batches = make(map[string]schema.BatchSnapshot, len(in.Batches))
	for key, value := range in.Batches {
		value.TaskIDs = append([]string{}, value.TaskIDs...)
		if value.HighWatermark != nil {
			hw := *value.HighWatermark
			value.HighWatermark = &hw
		}
		if value.HasLock != nil {
			lock := *value.HasLock
			value.HasLock = &lock
		}
		out.Batches[key] = value
	}
	out.Warnings = append([]schema.WarningSnapshot{}, in.Warnings...)
	return out
}

func Latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
