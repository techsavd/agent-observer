package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
)

const DefaultMaxFileSize int64 = 2 << 20

type Scanner struct {
	TasksDir    string
	TeamsDir    string
	MaxFileSize int64
	cache       map[string]cacheEntry
}

type cacheEntry struct {
	fingerprint fingerprint
	record      source.Record
}

type fingerprint struct {
	size    int64
	modTime time.Time
}

var filePattern = regexp.MustCompile(`(?:^|[\s"'(])([A-Za-z0-9_./-]+\.(?:go|ts|tsx|js|jsx|py|rs|swift|java|json|yaml|yml|md|css|html))`)

func NewScanner(tasksDir, teamsDir string, maxFileSize int64) *Scanner {
	if maxFileSize <= 0 {
		maxFileSize = DefaultMaxFileSize
	}
	return &Scanner{TasksDir: tasksDir, TeamsDir: teamsDir, MaxFileSize: maxFileSize, cache: map[string]cacheEntry{}}
}

func Scan(tasksDir, teamsDir string) schema.WorldSnapshot {
	return NewScanner(tasksDir, teamsDir, DefaultMaxFileSize).Scan()
}

func (s *Scanner) Scan() schema.WorldSnapshot {
	return s.ScanContext(context.Background())
}

func (s *Scanner) ScanContext(ctx context.Context) schema.WorldSnapshot {
	start := time.Now()
	records, warnings, stats := s.snapshotRecords(ctx)
	world := schema.WorldSnapshot{
		SchemaVersion: schema.CurrentSchemaVersion,
		Tasks:         map[string]schema.TaskSnapshot{},
		Batches:       map[string]schema.BatchSnapshot{},
		Warnings:      warnings,
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: record.Path, Message: err.Error()})
			break
		}
		switch record.Kind {
		case source.KindTaskJSON:
			task, err := normalizeTask(record)
			if err != nil {
				world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: record.Path, Message: err.Error()})
				continue
			}
			world.Tasks[task.ID] = task
			batch := world.Batches[task.BatchID]
			batch.BatchID = task.BatchID
			batch.TaskIDs = append(batch.TaskIDs, task.ID)
			addCount(&batch.Counts, task.Status)
			batch.LastUpdated = latest(batch.LastUpdated, task.LastUpdated)
			world.Batches[task.BatchID] = batch
		case source.KindBatchLock:
			batch := world.Batches[record.BatchID]
			batch.BatchID = record.BatchID
			value := true
			batch.HasLock = &value
			batch.LastUpdated = latest(batch.LastUpdated, record.ModTime)
			world.Batches[record.BatchID] = batch
		case source.KindHighWatermark:
			batch := world.Batches[record.BatchID]
			batch.BatchID = record.BatchID
			if value, err := strconv.Atoi(strings.TrimSpace(string(record.Payload))); err == nil {
				batch.HighWatermark = &value
			} else {
				world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: record.Path, Message: "invalid high-watermark: " + err.Error()})
			}
			batch.LastUpdated = latest(batch.LastUpdated, record.ModTime)
			world.Batches[record.BatchID] = batch
		}
	}
	for id, batch := range world.Batches {
		sort.Strings(batch.TaskIDs)
		world.Batches[id] = batch
	}
	stats.Warnings = len(world.Warnings)
	stats.LastDuration = time.Since(start)
	world.Stats = stats
	return world
}

func (s *Scanner) Events(world schema.WorldSnapshot) []schema.ObserverEvent {
	now := time.Now()
	events := make([]schema.ObserverEvent, 0, len(world.Tasks)+len(world.Batches))
	for _, batch := range world.Batches {
		events = append(events, schema.ObserverEvent{
			Type:       schema.EventBatchObserved,
			Source:     "claude",
			BatchID:    batch.BatchID,
			ObservedAt: now,
		})
		if batch.HasLock != nil {
			events = append(events, schema.ObserverEvent{Type: schema.EventBatchLockObserved, Source: "claude", BatchID: batch.BatchID, Attributes: map[string]string{"locked": strconv.FormatBool(*batch.HasLock)}, ObservedAt: now})
		}
		if batch.HighWatermark != nil {
			events = append(events, schema.ObserverEvent{Type: schema.EventBatchWatermark, Source: "claude", BatchID: batch.BatchID, Attributes: map[string]string{"high_watermark": strconv.Itoa(*batch.HighWatermark)}, ObservedAt: now})
		}
	}
	for _, task := range world.Tasks {
		events = append(events, schema.ObserverEvent{Type: schema.EventTaskObserved, Source: "claude", SourcePath: task.SourcePath, BatchID: task.BatchID, TaskID: task.ID, Status: task.Status, Role: task.Role, ObservedAt: now})
		if len(task.ActiveFiles) > 0 {
			events = append(events, schema.ObserverEvent{Type: schema.EventFilesTouched, Source: "claude", SourcePath: task.SourcePath, BatchID: task.BatchID, TaskID: task.ID, Files: task.ActiveFiles, ObservedAt: now})
		}
	}
	for _, warning := range world.Warnings {
		events = append(events, schema.ObserverEvent{Type: schema.EventWarningObserved, Source: "claude", SourcePath: warning.SourcePath, Attributes: map[string]string{"message": warning.Message}, ObservedAt: now})
	}
	return events
}

func (s *Scanner) snapshotRecords(ctx context.Context) ([]source.Record, []schema.WarningSnapshot, schema.ScanStats) {
	var records []source.Record
	var warnings []schema.WarningSnapshot
	var stats schema.ScanStats
	seen := map[string]bool{}
	defer s.pruneCache(seen)
	if err := ctx.Err(); err != nil {
		warnings = append(warnings, schema.WarningSnapshot{SourcePath: s.TasksDir, Message: err.Error()})
		return records, warnings, stats
	}
	if _, err := os.Stat(s.TeamsDir); err != nil {
		warnings = append(warnings, schema.WarningSnapshot{SourcePath: s.TeamsDir, Message: "optional teams root unavailable; running in task-batch fallback mode"})
	}
	entries, err := os.ReadDir(s.TasksDir)
	if err != nil {
		warnings = append(warnings, schema.WarningSnapshot{SourcePath: s.TasksDir, Message: err.Error()})
		return records, warnings, stats
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: s.TasksDir, Message: err.Error()})
			return records, warnings, stats
		}
		if !entry.IsDir() {
			continue
		}
		batchID := entry.Name()
		batchDir := filepath.Join(s.TasksDir, batchID)
		batchEntries, err := os.ReadDir(batchDir)
		if err != nil {
			warnings = append(warnings, schema.WarningSnapshot{SourcePath: batchDir, Message: err.Error()})
			continue
		}
		for _, batchEntry := range batchEntries {
			if batchEntry.IsDir() {
				continue
			}
			path := filepath.Join(batchDir, batchEntry.Name())
			record, ok, warning := s.readRecord(ctx, path, batchID, batchEntry.Name(), &stats)
			if warning != nil {
				warnings = append(warnings, *warning)
			}
			if ok {
				seen[path] = true
				records = append(records, record)
			}
		}
	}
	return records, warnings, stats
}

func (s *Scanner) readRecord(ctx context.Context, path, batchID, name string, stats *schema.ScanStats) (source.Record, bool, *schema.WarningSnapshot) {
	kind, taskIndex, ok := classify(name)
	if !ok {
		return source.Record{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: err.Error()}
	}
	before, err := os.Lstat(path)
	if err != nil {
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: err.Error()}
	}
	if before.Mode()&os.ModeSymlink != 0 {
		stats.SkippedSymlinks++
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: "skipped symlink"}
	}
	if before.Size() > s.MaxFileSize {
		stats.SkippedOversize++
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: fmt.Sprintf("skipped oversized file: %d bytes", before.Size())}
	}
	fp := fingerprint{size: before.Size(), modTime: before.ModTime()}
	if cached, ok := s.cache[path]; ok && cached.fingerprint == fp {
		stats.CacheHits++
		return cached.record, true, nil
	}
	if err := ctx.Err(); err != nil {
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: err.Error()}
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: err.Error()}
	}
	after, err := os.Lstat(path)
	if err != nil {
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: err.Error()}
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		stats.PartialRetries++
		return source.Record{}, false, &schema.WarningSnapshot{SourcePath: path, Message: "file changed during read; retrying next refresh"}
	}
	stats.FilesScanned++
	record := source.Record{
		Kind:      kind,
		Source:    "claude",
		Path:      path,
		BatchID:   batchID,
		TaskIndex: taskIndex,
		Payload:   payload,
		ModTime:   before.ModTime(),
		Size:      before.Size(),
	}
	s.cache[path] = cacheEntry{fingerprint: fp, record: record}
	return record, true, nil
}

func (s *Scanner) pruneCache(seen map[string]bool) {
	for path := range s.cache {
		if !seen[path] {
			delete(s.cache, path)
		}
	}
}

func classify(name string) (source.Kind, string, bool) {
	switch name {
	case ".lock":
		return source.KindBatchLock, "", true
	case ".highwatermark":
		return source.KindHighWatermark, "", true
	}
	if filepath.Ext(name) != ".json" {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ".json")
	if _, err := strconv.Atoi(stem); err != nil {
		return "", "", false
	}
	return source.KindTaskJSON, stem, true
}

func normalizeTask(record source.Record) (schema.TaskSnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return schema.TaskSnapshot{}, err
	}
	id := record.BatchID + ":" + record.TaskIndex
	title := firstString(raw, "title", "name", "subject", "active_form", "task")
	description := firstString(raw, "description", "prompt", "content", "summary")
	active := firstString(raw, "active_form", "activeForm", "current_task", "status_text")
	if title == "" {
		title = active
	}
	if title == "" {
		title = id
	}
	text := strings.Join([]string{title, description, active}, " ")
	return schema.TaskSnapshot{
		ID:          id,
		BatchID:     record.BatchID,
		Title:       title,
		Description: description,
		ActiveForm:  active,
		Status:      inferStatus(firstString(raw, "status", "state"), text),
		Role:        inferRole(text),
		ActiveFiles: extractFiles(text),
		SourcePath:  record.Path,
		LastUpdated: record.ModTime,
	}, nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			case json.Number:
				return typed.String()
			case fmt.Stringer:
				return strings.TrimSpace(typed.String())
			}
		}
	}
	return ""
}

func inferStatus(value, text string) schema.TaskStatus {
	candidate := strings.ToLower(value + " " + text)
	switch {
	case strings.Contains(candidate, "blocked"):
		return schema.StatusBlocked
	case strings.Contains(candidate, "error"), strings.Contains(candidate, "failed"):
		return schema.StatusErrored
	case strings.Contains(candidate, "complete"), strings.Contains(candidate, "done"):
		return schema.StatusCompleted
	case strings.Contains(candidate, "wait"), strings.Contains(candidate, "pending"), strings.Contains(candidate, "queued"):
		return schema.StatusWaiting
	case strings.Contains(candidate, "run"), strings.Contains(candidate, "progress"), strings.Contains(candidate, "active"):
		return schema.StatusRunning
	default:
		return schema.StatusUnknown
	}
}

func inferRole(text string) schema.AgentRole {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "lead"), strings.Contains(lower, "manager"):
		return schema.RoleLead
	case strings.Contains(lower, "review"):
		return schema.RoleReviewer
	case strings.Contains(lower, "qa"), strings.Contains(lower, "test"):
		return schema.RoleQA
	default:
		return schema.RoleCoding
	}
}

func extractFiles(text string) []schema.ActiveFile {
	matches := filePattern.FindAllStringSubmatch(text, -1)
	files := make([]schema.ActiveFile, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		path := strings.Trim(match[1], ".,;:")
		if !seen[path] {
			seen[path] = true
			files = append(files, schema.ActiveFile{Path: path})
		}
	}
	return files
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

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
