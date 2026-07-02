package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/techsavd/agent-observer/core/aggregate"
	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/store"
	"github.com/techsavd/agent-observer/internal/claude"
	"github.com/techsavd/agent-observer/internal/providers"
	claudeprovider "github.com/techsavd/agent-observer/internal/providers/claude"
	"github.com/techsavd/agent-observer/internal/tui"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type runConfig struct {
	TasksDir                    string `json:"tasks_dir"`
	TeamsDir                    string `json:"teams_dir"`
	MaxFileSize                 int64  `json:"max_file_size"`
	RefreshInterval             string `json:"refresh_interval"`
	Focus                       string `json:"focus"`
	Debug                       bool   `json:"debug"`
	WatchMode                   bool   `json:"watch_mode"`
	Shell                       bool   `json:"shell"`
	ShellEnabled                bool   `json:"shell_enabled"`
	Redact                      bool   `json:"redact"`
	LogFile                     string `json:"log_file,omitempty"`
	LogLevel                    string `json:"log_level"`
	TelemetryEnabled            bool   `json:"telemetry_enabled"`
	TelemetryEndpointConfigured bool   `json:"telemetry_endpoint_configured"`
}

type diagnostics struct {
	Version       string                   `json:"version"`
	Commit        string                   `json:"commit"`
	BuildDate     string                   `json:"build_date"`
	GoVersion     string                   `json:"go_version"`
	GOOS          string                   `json:"goos"`
	GOARCH        string                   `json:"goarch"`
	SchemaVersion string                   `json:"schema_version"`
	Config        runConfig                `json:"config"`
	Batches       int                      `json:"batches"`
	Tasks         int                      `json:"tasks"`
	Warnings      []schema.WarningSnapshot `json:"warnings"`
	Stats         schema.ScanStats         `json:"stats"`
}

func Run(ctx context.Context, args []string) error {
	home, _ := os.UserHomeDir()
	defaultTasksDir := filepath.Join(home, ".claude", "tasks")
	defaultTeamsDir := filepath.Join(home, ".claude", "teams")
	opts := options{
		command:         commandDashboard,
		claudeDir:       firstNonEmptyEnv("AGENT_OBSERVER_CLAUDE_DIR"),
		tasksDir:        firstNonEmptyEnv("AGENT_OBSERVER_TASKS_DIR", "CLAUDE_TASKS_DIR"),
		teamsDir:        firstNonEmptyEnv("AGENT_OBSERVER_TEAMS_DIR", "CLAUDE_TEAMS_DIR"),
		maxFileSize:     envInt64("AGENT_OBSERVER_MAX_FILE_SIZE", claude.DefaultMaxFileSize),
		refreshInterval: envDuration("AGENT_OBSERVER_REFRESH_INTERVAL", defaultRefreshInterval),
		shell:           envBool("AGENT_OBSERVER_SHELL", false),
		noShell:         envBool("AGENT_OBSERVER_NO_SHELL", false),
		redact:          envBool("AGENT_OBSERVER_REDACT", false),
		focus:           "all",
		logFile:         firstNonEmptyEnv("AGENT_OBSERVER_LOG_FILE"),
		logLevel:        firstNonEmptyEnv("AGENT_OBSERVER_LOG_LEVEL"),
		telemetry:       firstNonEmptyEnv("AGENT_OBSERVER_TELEMETRY"),
		telemetryURL:    firstNonEmptyEnv("AGENT_OBSERVER_TELEMETRY_ENDPOINT"),
	}
	if opts.tasksDir == "" {
		opts.tasksDir = defaultTasksDir
	}
	if opts.teamsDir == "" {
		opts.teamsDir = defaultTeamsDir
	}
	if opts.logLevel == "" {
		opts.logLevel = "info"
	}
	parseArgs := args
	if len(args) > 0 && isCommand(args[0]) {
		opts.command = command(args[0])
		parseArgs = args[1:]
	}
	fs := flag.NewFlagSet("agent-observer", flag.ContinueOnError)
	fs.Usage = func() {
		printUsage(fs.Output())
		fs.PrintDefaults()
	}
	fs.StringVar(&opts.claudeDir, "claude-dir", opts.claudeDir, "Claude Code state directory (default ~/.claude)")
	fs.StringVar(&opts.tasksDir, "tasks-dir", opts.tasksDir, "Claude tasks directory")
	fs.StringVar(&opts.teamsDir, "teams-dir", opts.teamsDir, "Claude teams directory")
	fs.Int64Var(&opts.maxFileSize, "max-file-size", opts.maxFileSize, "maximum bytes to read per file")
	fs.DurationVar(&opts.refreshInterval, "refresh-interval", opts.refreshInterval, "TUI refresh interval")
	fs.BoolVar(&opts.debug, "debug", false, "show debug UI")
	fs.BoolVar(&opts.dumpJSON, "dump-json", false, "dump snapshot JSON and exit")
	fs.BoolVar(&opts.dumpText, "dump-text", false, "dump snapshot text and exit")
	fs.BoolVar(&opts.dumpDiagnostics, "diagnostics", false, "dump diagnostics JSON and exit")
	fs.BoolVar(&opts.showVersion, "version", false, "print version and exit")
	fs.BoolVar(&opts.shell, "shell", opts.shell, "enable the local shell pane")
	fs.BoolVar(&opts.noShell, "no-shell", opts.noShell, "disable the local shell pane")
	fs.BoolVar(&opts.redact, "redact", opts.redact, "redact local paths in dump, diagnostics, and doctor output")
	fs.StringVar(&opts.logFile, "log-file", opts.logFile, "append structured logs to this file")
	fs.StringVar(&opts.logLevel, "log-level", opts.logLevel, "log level: debug, info, warn, error")
	fs.StringVar(&opts.telemetry, "telemetry", opts.telemetry, "telemetry mode: off, on")
	fs.StringVar(&opts.telemetryURL, "telemetry-endpoint", opts.telemetryURL, "telemetry endpoint URL")
	fs.StringVar(&opts.focus, "focus", opts.focus, "dump-text focus: all, active, blocked, warnings")
	if err := fs.Parse(parseArgs); err != nil {
		return err
	}
	markSeenFlags(fs, &opts)
	normalizeShellOptions(&opts)
	if opts.showVersion {
		fmt.Fprintln(os.Stdout, VersionString())
		return nil
	}
	positionals := fs.Args()
	if len(positionals) > 0 {
		if isCommand(positionals[0]) {
			if opts.command != commandDashboard {
				return fmt.Errorf("duplicate command %q", positionals[0])
			}
			opts.command = command(positionals[0])
		} else {
			return fmt.Errorf("unknown command %q", positionals[0])
		}
	}
	if len(positionals) > 1 {
		return fmt.Errorf("unexpected argument %q", positionals[1])
	}
	if err := validateOptions(&opts); err != nil {
		return err
	}
	logger, cleanup, err := setupLogger(opts.logFile, opts.logLevel)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer cleanup()
	logger.Info("starting",
		slog.String("version", Version),
		slog.String("command", string(opts.command)),
		slog.String("tasks_dir", opts.tasksDir),
		slog.String("teams_dir", opts.teamsDir),
	)
	adapters := providers.Build(providers.Config{
		Claude: claudeprovider.Config{
			ClaudeDir:   opts.claudeDir,
			TasksDir:    opts.tasksDir,
			TeamsDir:    opts.teamsDir,
			MaxFileSize: opts.maxFileSize,
		},
	})
	memory := store.NewMemoryStore()
	refresh := func(ctx context.Context) schema.WorldSnapshot {
		snaps := make([]source.ProviderSnapshot, 0, len(adapters))
		for _, adapter := range adapters {
			snaps = append(snaps, adapter.Snapshot(ctx))
		}
		world := memory.Replace(aggregate.Merge(snaps))
		logger.Debug("scan complete",
			slog.Int("sessions", len(world.Sessions)),
			slog.Int("tasks", len(world.Tasks)),
			slog.Int("batches", len(world.Batches)),
			slog.Int("warnings", len(world.Warnings)),
			slog.Duration("duration", world.Stats.LastDuration),
		)
		return world
	}
	world := refresh(ctx)
	telemetry := newTelemetryClient(opts)
	trackTelemetry(ctx, logger, telemetry, buildTelemetryEvent("app.start", opts, world, ""))
	trackTelemetry(ctx, logger, telemetry, buildTelemetryEvent("scan.summary", opts, world, ""))
	config := runConfig{
		TasksDir:                    opts.tasksDir,
		TeamsDir:                    opts.teamsDir,
		MaxFileSize:                 opts.maxFileSize,
		RefreshInterval:             opts.refreshInterval.String(),
		Focus:                       opts.focus,
		Debug:                       opts.debug,
		WatchMode:                   opts.command == commandWatch,
		Shell:                       shellEnabled(opts),
		ShellEnabled:                shellEnabled(opts),
		Redact:                      opts.redact,
		LogFile:                     opts.logFile,
		LogLevel:                    opts.logLevel,
		TelemetryEnabled:            opts.telemetry == "on" && opts.telemetryURL != "",
		TelemetryEndpointConfigured: opts.telemetryURL != "",
	}
	outputWorld := world
	outputConfig := config
	if opts.redact {
		outputWorld = redactWorld(world, config)
		outputConfig = redactConfig(config)
	}
	if opts.command == commandDoctor {
		report := buildDoctorReport(config, world)
		if opts.redact {
			report = redactDoctorReport(report, config)
		}
		if opts.dumpJSON {
			err = dumpDoctorJSON(os.Stdout, report)
		} else {
			err = dumpDoctorText(os.Stdout, report)
		}
		if err != nil {
			return err
		}
		if doctorHasFailure(report) {
			return fmt.Errorf("doctor checks failed")
		}
		return nil
	}
	if opts.dumpJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(outputWorld)
	}
	if opts.dumpDiagnostics {
		return dumpDiagnosticsJSON(os.Stdout, outputConfig, outputWorld)
	}
	if opts.dumpText {
		return dumpTextSummary(os.Stdout, outputWorld, opts.focus)
	}
	model := tui.New(world, opts.debug, refresh).WithRefreshInterval(opts.refreshInterval)
	if opts.command == commandWatch {
		model = tui.NewWatch(world, opts.debug, refresh).WithRefreshInterval(opts.refreshInterval)
	}
	model = model.WithShellEnabled(shellEnabled(opts))
	_, err = tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		trackTelemetry(context.Background(), logger, telemetry, buildTelemetryEvent("app.error", opts, world, errorCategory(err)))
	}
	return err
}

func isCommand(value string) bool {
	switch value {
	case string(commandWatch), string(commandDoctor):
		return true
	default:
		return false
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintf(out, `Agent Observer watches local Claude Code task state.

Usage:
  agent-observer [flags]
  agent-observer watch [flags]
  agent-observer doctor [flags]

Commands:
  watch    start the live dashboard in watch mode
  doctor   validate configuration and print a support report

Flags:
`)
}

func VersionString() string {
	return fmt.Sprintf("agent-observer %s (%s, built %s)", Version, Commit, BuildDate)
}

func dumpDiagnosticsJSON(out io.Writer, config runConfig, world schema.WorldSnapshot) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(diagnostics{
		Version:       Version,
		Commit:        Commit,
		BuildDate:     BuildDate,
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		SchemaVersion: schema.CurrentSchemaVersion,
		Config:        config,
		Batches:       len(world.Batches),
		Tasks:         len(world.Tasks),
		Warnings:      world.Warnings,
		Stats:         world.Stats,
	})
}

func dumpTextSummary(out io.Writer, world schema.WorldSnapshot, focus string) error {
	fmt.Fprintf(out, "Agent Observer Snapshot\n")
	fmt.Fprintf(out, "Batches: %d\nTasks: %d\nWarnings: %d\n", len(world.Batches), len(world.Tasks), len(world.Warnings))
	fmt.Fprintf(out, "Refresh: files=%d cache=%d warnings=%d duration=%s\n", world.Stats.FilesScanned, world.Stats.CacheHits, world.Stats.Warnings, world.Stats.LastDuration.Round(time.Millisecond))
	if focus == "warnings" {
		return dumpWarnings(out, world)
	}
	batches := make([]schema.BatchSnapshot, 0, len(world.Batches))
	for _, batch := range world.Batches {
		if focus == "active" && inactive(batch) {
			continue
		}
		if focus == "blocked" && batch.Counts.Blocked == 0 && batch.Counts.Errored == 0 {
			continue
		}
		batches = append(batches, batch)
	}
	sort.Slice(batches, func(i, j int) bool {
		left, right := batchPriority(batches[i]), batchPriority(batches[j])
		if left != right {
			return left < right
		}
		return batches[i].LastUpdated.After(batches[j].LastUpdated)
	})
	fmt.Fprintln(out, "\nBatches")
	if len(batches) == 0 {
		fmt.Fprintln(out, "- none")
	}
	for _, batch := range batches {
		fmt.Fprintf(out, "- %s run:%d wait:%d block:%d done:%d err:%d\n", batch.BatchID, batch.Counts.Running, batch.Counts.Waiting, batch.Counts.Blocked, batch.Counts.Completed, batch.Counts.Errored)
		tasks := tasksForBatch(world, batch.BatchID)
		for _, task := range tasks {
			if focus == "blocked" && task.Status != schema.StatusBlocked && task.Status != schema.StatusErrored {
				continue
			}
			fmt.Fprintf(out, "  * [%s] %s\n", strings.ToUpper(string(task.Status)), firstNonEmpty(task.ActiveForm, task.Title, task.ID))
		}
	}
	if len(world.Warnings) > 0 && focus == "all" {
		return dumpWarnings(out, world)
	}
	return nil
}

func dumpWarnings(out io.Writer, world schema.WorldSnapshot) error {
	fmt.Fprintln(out, "\nWarnings")
	if len(world.Warnings) == 0 {
		fmt.Fprintln(out, "- none")
		return nil
	}
	for _, warning := range world.Warnings {
		fmt.Fprintf(out, "- %s: %s\n", warning.SourcePath, warning.Message)
	}
	return nil
}

func tasksForBatch(world schema.WorldSnapshot, batchID string) []schema.TaskSnapshot {
	tasks := make([]schema.TaskSnapshot, 0)
	for _, task := range world.Tasks {
		if task.BatchID == batchID {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		left, right := taskPriority(tasks[i].Status), taskPriority(tasks[j].Status)
		if left != right {
			return left < right
		}
		if !tasks[i].LastUpdated.Equal(tasks[j].LastUpdated) {
			return tasks[i].LastUpdated.After(tasks[j].LastUpdated)
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "-"
}

func inactive(batch schema.BatchSnapshot) bool {
	return batch.Counts.Running == 0 && batch.Counts.Waiting == 0 && batch.Counts.Blocked == 0 && batch.Counts.Errored == 0
}

func batchPriority(batch schema.BatchSnapshot) int {
	switch {
	case batch.Counts.Running > 0:
		return 0
	case batch.Counts.Waiting > 0:
		return 1
	case batch.Counts.Blocked > 0:
		return 2
	case batch.Counts.Errored > 0:
		return 3
	default:
		return 4
	}
}

func taskPriority(status schema.TaskStatus) int {
	switch status {
	case schema.StatusRunning:
		return 0
	case schema.StatusWaiting:
		return 1
	case schema.StatusBlocked:
		return 2
	case schema.StatusErrored:
		return 3
	case schema.StatusCompleted:
		return 4
	default:
		return 5
	}
}
