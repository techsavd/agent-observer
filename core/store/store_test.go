package store

import (
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

func TestMemoryStoreReplaceAndSnapshot(t *testing.T) {
	store := NewMemoryStore()
	world := store.Replace(schema.WorldSnapshot{
		SchemaVersion: schema.CurrentSchemaVersion,
		Tasks: map[string]schema.TaskSnapshot{
			"b:1": {ID: "b:1", BatchID: "b", Status: schema.StatusRunning, LastUpdated: time.Now()},
		},
		Batches: map[string]schema.BatchSnapshot{
			"b": {BatchID: "b", TaskIDs: []string{"b:1"}},
		},
		Sessions: map[string]schema.SessionSnapshot{
			"claude:s1": {ID: "s1", Provider: "claude", Status: schema.SessionBusy},
		},
	})
	if len(world.Tasks) != 1 || len(world.Sessions) != 1 {
		t.Fatalf("unexpected world: %#v", world)
	}
	got := store.Snapshot()
	if got.Sessions["claude:s1"].Status != schema.SessionBusy {
		t.Fatalf("unexpected session: %#v", got.Sessions["claude:s1"])
	}
}

func TestMemoryStoreSnapshotsAreIndependentCopies(t *testing.T) {
	store := NewMemoryStore()
	store.Replace(schema.WorldSnapshot{
		Tasks: map[string]schema.TaskSnapshot{"b:1": {ID: "b:1", BatchID: "b"}},
	})
	first := store.Snapshot()
	first.Tasks["b:1"] = schema.TaskSnapshot{ID: "mutated"}
	second := store.Snapshot()
	if second.Tasks["b:1"].ID != "b:1" {
		t.Fatal("snapshot mutation leaked into store")
	}
}

func TestMemoryStoreWarningsDoNotAccumulate(t *testing.T) {
	store := NewMemoryStore()
	store.Replace(schema.WorldSnapshot{Warnings: []schema.WarningSnapshot{{SourcePath: "a", Message: "first"}}})
	world := store.Replace(schema.WorldSnapshot{})
	if len(world.Warnings) != 0 {
		t.Fatalf("expected warnings to be replaced, got %#v", world.Warnings)
	}
}
