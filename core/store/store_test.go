package store

import (
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

func TestMemoryStoreRebuildsBatchCounts(t *testing.T) {
	store := NewMemoryStore()
	world := store.Replace(map[string]schema.TaskSnapshot{
		"b:1": {ID: "b:1", BatchID: "b", Status: schema.StatusRunning, LastUpdated: time.Now()},
		"b:2": {ID: "b:2", BatchID: "b", Status: schema.StatusCompleted, LastUpdated: time.Now()},
	}, nil, schema.ScanStats{})
	batch := world.Batches["b"]
	if batch.Counts.Running != 1 || batch.Counts.Completed != 1 {
		t.Fatalf("unexpected counts: %#v", batch.Counts)
	}
	if len(batch.TaskIDs) != 2 {
		t.Fatalf("expected two task ids, got %#v", batch.TaskIDs)
	}
}

func TestMemoryStoreWarningsDoNotAccumulate(t *testing.T) {
	store := NewMemoryStore()
	store.Replace(nil, []schema.WarningSnapshot{{SourcePath: "a", Message: "first"}}, schema.ScanStats{})
	world := store.Replace(nil, nil, schema.ScanStats{})
	if len(world.Warnings) != 0 {
		t.Fatalf("expected warnings to be replaced, got %#v", world.Warnings)
	}
}
