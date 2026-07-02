package aggregate

import (
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
)

func TestMergeEmpty(t *testing.T) {
	world := Merge(nil)
	if world.SchemaVersion != schema.CurrentSchemaVersion {
		t.Fatalf("schema version = %q, want %q", world.SchemaVersion, schema.CurrentSchemaVersion)
	}
	if world.Tasks == nil || world.Batches == nil || world.Sessions == nil || world.Providers == nil {
		t.Fatal("merged world must have non-nil maps")
	}
}

func TestMergeNamespacesSessionsAndKeepsTaskKeys(t *testing.T) {
	claude := source.ProviderSnapshot{
		Provider: "claude",
		Info:     schema.ProviderInfo{Name: "claude", Available: true},
		Sessions: map[string]schema.SessionSnapshot{
			"abc": {ID: "abc", Status: schema.SessionBusy},
		},
		Tasks: map[string]schema.TaskSnapshot{
			"batch-1:1": {ID: "batch-1:1", BatchID: "batch-1"},
		},
		Batches: map[string]schema.BatchSnapshot{
			"batch-1": {BatchID: "batch-1"},
		},
	}
	codex := source.ProviderSnapshot{
		Provider: "codex",
		Info:     schema.ProviderInfo{Name: "codex", Available: true},
		Sessions: map[string]schema.SessionSnapshot{
			"abc": {ID: "abc", Status: schema.SessionIdle},
		},
	}
	world := Merge([]source.ProviderSnapshot{claude, codex})
	if len(world.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(world.Sessions))
	}
	if _, ok := world.Sessions["claude:abc"]; !ok {
		t.Fatal("expected session key claude:abc")
	}
	if _, ok := world.Sessions["codex:abc"]; !ok {
		t.Fatal("expected session key codex:abc")
	}
	if world.Sessions["claude:abc"].Provider != "claude" {
		t.Fatalf("provider = %q, want claude", world.Sessions["claude:abc"].Provider)
	}
	if _, ok := world.Tasks["batch-1:1"]; !ok {
		t.Fatal("task keys must pass through un-namespaced")
	}
	if world.Tasks["batch-1:1"].Provider != "claude" {
		t.Fatal("task must be stamped with provider")
	}
	if world.Batches["batch-1"].Provider != "claude" {
		t.Fatal("batch must be stamped with provider")
	}
	if len(world.Providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(world.Providers))
	}
}

func TestMergeTaskKeyCollisionGetsPrefixed(t *testing.T) {
	a := source.ProviderSnapshot{
		Provider: "a",
		Tasks:    map[string]schema.TaskSnapshot{"x:1": {ID: "x:1"}},
		Batches:  map[string]schema.BatchSnapshot{"x": {BatchID: "x"}},
	}
	b := source.ProviderSnapshot{
		Provider: "b",
		Tasks:    map[string]schema.TaskSnapshot{"x:1": {ID: "x:1"}},
		Batches:  map[string]schema.BatchSnapshot{"x": {BatchID: "x"}},
	}
	world := Merge([]source.ProviderSnapshot{a, b})
	if len(world.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2 (collision must not drop)", len(world.Tasks))
	}
	if _, ok := world.Tasks["b:x:1"]; !ok {
		t.Fatal("colliding task from second provider must be prefixed b:")
	}
	if _, ok := world.Batches["b:x"]; !ok {
		t.Fatal("colliding batch from second provider must be prefixed b:")
	}
}

func TestMergeSumsStatsAndConcatsWarnings(t *testing.T) {
	a := source.ProviderSnapshot{
		Provider: "a",
		Warnings: []schema.WarningSnapshot{{SourcePath: "p1", Message: "m1"}},
		Stats:    schema.ScanStats{FilesScanned: 2, CacheHits: 1, LastDuration: 5 * time.Millisecond},
	}
	b := source.ProviderSnapshot{
		Provider: "b",
		Warnings: []schema.WarningSnapshot{{SourcePath: "p2", Message: "m2"}},
		Stats:    schema.ScanStats{FilesScanned: 3, CacheHits: 4, LastDuration: 7 * time.Millisecond},
	}
	world := Merge([]source.ProviderSnapshot{a, b})
	if world.Stats.FilesScanned != 5 || world.Stats.CacheHits != 5 {
		t.Fatalf("stats not summed: %+v", world.Stats)
	}
	if world.Stats.LastDuration != 12*time.Millisecond {
		t.Fatalf("durations not summed: %s", world.Stats.LastDuration)
	}
	if len(world.Warnings) != 2 {
		t.Fatalf("warnings = %d, want 2", len(world.Warnings))
	}
	if world.Stats.Warnings != 2 {
		t.Fatalf("stats.Warnings = %d, want 2", world.Stats.Warnings)
	}
}
