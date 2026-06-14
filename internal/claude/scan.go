package claude

import (
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
)

var filePattern = regexp.MustCompile(`(?:^|[\s"'(])([A-Za-z0-9_./-]+\.(?:go|ts|tsx|js|jsx|py|rs|swift|java|json|yaml|yml|md|css|html))`)

func Scan(tasksDir, teamsDir string) schema.WorldSnapshot {
	start := time.Now()
	world := schema.WorldSnapshot{
		Tasks:   map[string]schema.TaskSnapshot{},
		Batches: map[string]schema.BatchSnapshot{},
	}
	if _, err := os.Stat(teamsDir); err != nil {
		world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: teamsDir, Message: "optional teams root unavailable; running in task-batch fallback mode"})
	}
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: tasksDir, Message: err.Error()})
		world.Stats.LastDuration = time.Since(start)
		world.Stats.Warnings = len(world.Warnings)
		return world
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scanBatch(filepath.Join(tasksDir, entry.Name()), entry.Name(), &world)
	}
	world.Stats.LastDuration = time.Since(start)
	world.Stats.Warnings = len(world.Warnings)
	return world
}

func scanBatch(dir, batchID string, world *schema.WorldSnapshot) {
	batch := schema.BatchSnapshot{BatchID: batchID}
	entries, err := os.ReadDir(dir)
	if err != nil {
		world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: dir, Message: err.Error()})
		return
	}
	lockValue := false
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err == nil && info.ModTime().After(batch.LastUpdated) {
			batch.LastUpdated = info.ModTime()
		}
		switch entry.Name() {
		case ".lock":
			lockValue = true
			batch.HasLock = &lockValue
			continue
		case ".highwatermark":
			value, err := readInt(path)
			if err == nil {
				batch.HighWatermark = &value
			}
			continue
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		task, err := parseTask(path, batchID, entry.Name())
		world.Stats.FilesScanned++
		if err != nil {
			world.Warnings = append(world.Warnings, schema.WarningSnapshot{SourcePath: path, Message: err.Error()})
			continue
		}
		world.Tasks[task.ID] = task
		batch.TaskIDs = append(batch.TaskIDs, task.ID)
		addCount(&batch.Counts, task.Status)
		if task.LastUpdated.After(batch.LastUpdated) {
			batch.LastUpdated = task.LastUpdated
		}
	}
	sort.Strings(batch.TaskIDs)
	world.Batches[batchID] = batch
}

func parseTask(path, batchID, name string) (schema.TaskSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schema.TaskSnapshot{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return schema.TaskSnapshot{}, err
	}
	info, _ := os.Stat(path)
	updated := time.Now()
	if info != nil {
		updated = info.ModTime()
	}
	id := batchID + ":" + strings.TrimSuffix(name, filepath.Ext(name))
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
		BatchID:     batchID,
		Title:       title,
		Description: description,
		ActiveForm:  active,
		Status:      inferStatus(firstString(raw, "status", "state"), text),
		Role:        inferRole(text),
		ActiveFiles: extractFiles(text),
		SourcePath:  path,
		LastUpdated: updated,
	}, nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
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

func readInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}
