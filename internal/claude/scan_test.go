package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/techsavd/agent-observer/core/schema"
)

func TestScannerMapsFixtureBatchesAndMetadata(t *testing.T) {
	scanner := fixtureScanner()
	world := scanner.Scan()
	if world.SchemaVersion != schema.CurrentSchemaVersion {
		t.Fatalf("expected schema version %q, got %q", schema.CurrentSchemaVersion, world.SchemaVersion)
	}
	batch := world.Batches["active-batch"]
	if batch.BatchID != "active-batch" {
		t.Fatalf("expected active batch, got %#v", batch)
	}
	if batch.Counts.Running != 1 || batch.Counts.Waiting != 1 {
		t.Fatalf("expected running/waiting counts, got %#v", batch.Counts)
	}
	if batch.HighWatermark == nil || *batch.HighWatermark != 2 {
		t.Fatalf("expected high-watermark 2, got %#v", batch.HighWatermark)
	}
	if batch.HasLock == nil || !*batch.HasLock {
		t.Fatalf("expected lock present")
	}
	task := world.Tasks["active-batch:1"]
	if task.Role != schema.RoleCoding {
		t.Fatalf("expected coding role, got %s", task.Role)
	}
	if len(task.ActiveFiles) != 2 {
		t.Fatalf("expected extracted source files, got %#v", task.ActiveFiles)
	}
}

func TestScannerPrunesDeletedFilesFromCache(t *testing.T) {
	tasksDir := t.TempDir()
	teamsDir := filepath.Join(t.TempDir(), "missing-teams")
	batchDir := filepath.Join(tasksDir, "batch")
	if err := os.Mkdir(batchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(batchDir, "1.json")
	if err := os.WriteFile(taskPath, []byte(`{"title":"running task","status":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(tasksDir, teamsDir, DefaultMaxFileSize)
	first := scanner.Scan()
	if _, ok := first.Tasks["batch:1"]; !ok {
		t.Fatalf("expected task on first scan, got %#v", first.Tasks)
	}
	if err := os.Remove(taskPath); err != nil {
		t.Fatal(err)
	}
	second := scanner.Scan()
	if _, ok := second.Tasks["batch:1"]; ok {
		t.Fatalf("expected deleted task to disappear, got %#v", second.Tasks)
	}
	if len(scanner.cache) != 0 {
		t.Fatalf("expected cache to prune deleted file, got %#v", scanner.cache)
	}
}

func TestScannerSkipsTaskSymlinks(t *testing.T) {
	tasksDir := t.TempDir()
	teamsDir := filepath.Join(t.TempDir(), "missing-teams")
	batchDir := filepath.Join(tasksDir, "batch")
	if err := os.Mkdir(batchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(batchDir, "target.json")
	if err := os.WriteFile(target, []byte(`{"title":"linked task","status":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(batchDir, "1.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	world := NewScanner(tasksDir, teamsDir, DefaultMaxFileSize).Scan()
	if len(world.Tasks) != 0 {
		t.Fatalf("expected symlinked task to be skipped, got %#v", world.Tasks)
	}
	if world.Stats.SkippedSymlinks != 1 {
		t.Fatalf("expected skipped symlink count, got %#v", world.Stats)
	}
	if !warningsContain(world.Warnings, "skipped symlink") {
		t.Fatalf("expected symlink warning, got %#v", world.Warnings)
	}
}

func TestScannerSkipsOversizedTaskFiles(t *testing.T) {
	tasksDir := t.TempDir()
	teamsDir := filepath.Join(t.TempDir(), "missing-teams")
	batchDir := filepath.Join(tasksDir, "batch")
	if err := os.Mkdir(batchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(batchDir, "1.json"), []byte(`{"title":"too large"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	world := NewScanner(tasksDir, teamsDir, 4).Scan()
	if len(world.Tasks) != 0 {
		t.Fatalf("expected oversized task to be skipped, got %#v", world.Tasks)
	}
	if world.Stats.SkippedOversize != 1 {
		t.Fatalf("expected oversized count, got %#v", world.Stats)
	}
	if !warningsContain(world.Warnings, "skipped oversized file") {
		t.Fatalf("expected oversized warning, got %#v", world.Warnings)
	}
}

func TestScannerHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	world := fixtureScanner().ScanContext(ctx)
	if world.SchemaVersion != schema.CurrentSchemaVersion {
		t.Fatalf("expected schema version %q, got %q", schema.CurrentSchemaVersion, world.SchemaVersion)
	}
	if !warningsContain(world.Warnings, context.Canceled.Error()) {
		t.Fatalf("expected canceled context warning, got %#v", world.Warnings)
	}
}

func TestScannerWarnsOnMalformedJSONAndMissingTeams(t *testing.T) {
	world := fixtureScanner().Scan()
	if len(world.Warnings) < 2 {
		t.Fatalf("expected missing teams and malformed warnings, got %#v", world.Warnings)
	}
}

func TestScannerCacheHitOnSecondScan(t *testing.T) {
	scanner := fixtureScanner()
	_ = scanner.Scan()
	world := scanner.Scan()
	if world.Stats.CacheHits == 0 {
		t.Fatalf("expected cache hits on second scan, got %#v", world.Stats)
	}
}

func TestScannerProducesPortableEvents(t *testing.T) {
	scanner := fixtureScanner()
	world := scanner.Scan()
	events := scanner.Events(world)
	if len(events) == 0 {
		t.Fatalf("expected events")
	}
	var sawBatch, sawTask, sawFiles bool
	for _, event := range events {
		switch event.Type {
		case schema.EventBatchObserved:
			sawBatch = true
		case schema.EventTaskObserved:
			sawTask = true
		case schema.EventFilesTouched:
			sawFiles = true
		}
	}
	if !sawBatch || !sawTask || !sawFiles {
		t.Fatalf("expected batch/task/files events, got %#v", events)
	}
}

func fixtureScanner() *Scanner {
	return NewScanner(
		filepath.Join("..", "testdata", "claude", "tasks"),
		filepath.Join("..", "testdata", "claude", "missing-teams"),
		DefaultMaxFileSize,
	)
}

func warningsContain(warnings []schema.WarningSnapshot, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning.Message, needle) {
			return true
		}
	}
	return false
}
