package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

type doctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type doctorReport struct {
	Version       string                   `json:"version"`
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
	Config        runConfig                `json:"config"`
	Checks        []doctorCheck            `json:"checks"`
	Sessions      int                      `json:"sessions"`
	Batches       int                      `json:"batches"`
	Tasks         int                      `json:"tasks"`
	Warnings      []schema.WarningSnapshot `json:"warnings"`
	Stats         schema.ScanStats         `json:"stats"`
}

func buildDoctorReport(config runConfig, world schema.WorldSnapshot) doctorReport {
	checks := []doctorCheck{
		pathCheck("tasks_dir", config.TasksDir, true),
		pathCheck("teams_dir", config.TeamsDir, false),
		valueCheck("max_file_size", config.MaxFileSize > 0, fmt.Sprintf("%d bytes", config.MaxFileSize)),
		valueCheck("scan", true, fmt.Sprintf("completed with %d warnings", len(world.Warnings))),
	}
	checks = append(checks, providerChecks(world)...)
	if config.ActEnabled {
		checks = append(checks, doctorCheck{
			Name:    "act",
			OK:      true,
			Message: "agent actions ENABLED: the dashboard can execute provider CLIs listed above as this user",
		})
	}
	return doctorReport{
		Version:       VersionString(),
		SchemaVersion: schema.CurrentSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Config:        config,
		Checks:        checks,
		Sessions:      len(world.Sessions),
		Batches:       len(world.Batches),
		Tasks:         len(world.Tasks),
		Warnings:      world.Warnings,
		Stats:         world.Stats,
	}
}

// providerChecks reports each provider's detection state. A provider that is
// not installed is informational, never a doctor failure.
func providerChecks(world schema.WorldSnapshot) []doctorCheck {
	names := make([]string, 0, len(world.Providers))
	for name := range world.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	checks := make([]doctorCheck, 0, len(names))
	for _, name := range names {
		info := world.Providers[name]
		message := "not detected"
		if info.Available {
			message = "detected"
			if info.CLIPath != "" {
				message += ", cli " + info.CLIPath
			}
		}
		checks = append(checks, doctorCheck{Name: "provider:" + name, OK: true, Message: message})
	}
	return checks
}

func pathCheck(name, path string, required bool) doctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		if required {
			return doctorCheck{Name: name, OK: false, Message: err.Error()}
		}
		return doctorCheck{Name: name, OK: true, Message: "optional path unavailable: " + err.Error()}
	}
	if !info.IsDir() {
		return doctorCheck{Name: name, OK: false, Message: "not a directory"}
	}
	if _, err := os.ReadDir(path); err != nil {
		return doctorCheck{Name: name, OK: false, Message: err.Error()}
	}
	return doctorCheck{Name: name, OK: true, Message: "readable"}
}

func valueCheck(name string, ok bool, message string) doctorCheck {
	return doctorCheck{Name: name, OK: ok, Message: message}
}

func dumpDoctorJSON(out io.Writer, report doctorReport) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(report)
}

func dumpDoctorText(out io.Writer, report doctorReport) error {
	fmt.Fprintln(out, "Agent Observer Doctor")
	fmt.Fprintf(out, "Version: %s\n", report.Version)
	fmt.Fprintf(out, "Schema: %s\n", report.SchemaVersion)
	fmt.Fprintf(out, "Sessions: %d\nTasks: %d\nBatches: %d\nWarnings: %d\n", report.Sessions, report.Tasks, report.Batches, len(report.Warnings))
	fmt.Fprintln(out, "\nChecks")
	for _, check := range report.Checks {
		status := "ok"
		if !check.OK {
			status = "fail"
		}
		fmt.Fprintf(out, "- %s: %s (%s)\n", check.Name, status, check.Message)
	}
	if len(report.Warnings) > 0 {
		world := schema.WorldSnapshot{Warnings: report.Warnings}
		return dumpWarnings(out, world)
	}
	return nil
}

func doctorHasFailure(report doctorReport) bool {
	for _, check := range report.Checks {
		if !check.OK {
			return true
		}
	}
	return false
}
