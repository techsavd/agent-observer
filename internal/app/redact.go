package app

import (
	"path/filepath"
	"strings"

	"github.com/techsavd/agent-observer/core/schema"
)

func redactWorld(world schema.WorldSnapshot, config runConfig) schema.WorldSnapshot {
	roots := []string{config.TasksDir, config.TeamsDir}
	out := world
	out.Tasks = make(map[string]schema.TaskSnapshot, len(world.Tasks))
	for key, task := range world.Tasks {
		task.ActiveFiles = append([]schema.ActiveFile{}, task.ActiveFiles...)
		task.SourcePath = redactPath(task.SourcePath)
		for i := range task.ActiveFiles {
			task.ActiveFiles[i].Path = redactPath(task.ActiveFiles[i].Path)
		}
		out.Tasks[key] = task
	}
	out.Batches = make(map[string]schema.BatchSnapshot, len(world.Batches))
	for key, batch := range world.Batches {
		batch.TaskIDs = append([]string{}, batch.TaskIDs...)
		out.Batches[key] = batch
	}
	out.Warnings = make([]schema.WarningSnapshot, len(world.Warnings))
	for i, warning := range world.Warnings {
		warning.SourcePath = redactPath(warning.SourcePath)
		warning.Message = redactText(warning.Message, roots)
		out.Warnings[i] = warning
	}
	return out
}

func redactConfig(config runConfig) runConfig {
	config.TasksDir = redactPath(config.TasksDir)
	config.TeamsDir = redactPath(config.TeamsDir)
	if config.LogFile != "" {
		config.LogFile = redactPath(config.LogFile)
	}
	return config
}

func redactDoctorReport(report doctorReport, config runConfig) doctorReport {
	roots := []string{config.TasksDir, config.TeamsDir, config.LogFile}
	report.Config = redactConfig(report.Config)
	for i := range report.Checks {
		report.Checks[i].Message = redactText(report.Checks[i].Message, roots)
	}
	for i := range report.Warnings {
		report.Warnings[i].SourcePath = redactPath(report.Warnings[i].SourcePath)
		report.Warnings[i].Message = redactText(report.Warnings[i].Message, roots)
	}
	return report
}

func redactPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return "<redacted>"
	}
	return filepath.Join("<redacted>", base)
}

func redactText(text string, roots []string) string {
	out := text
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root != "" {
			out = strings.ReplaceAll(out, root, "<redacted>")
		}
	}
	return out
}
