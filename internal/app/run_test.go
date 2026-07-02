package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/internal/claude"
)

func TestDumpTextFocusActive(t *testing.T) {
	world := fixtureWorld(t)
	var out bytes.Buffer
	if err := dumpTextSummary(&out, world, "active"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "active-batch") {
		t.Fatalf("expected active batch, got:\n%s", text)
	}
	if strings.Contains(text, "inactive-batch") {
		t.Fatalf("expected inactive batch hidden, got:\n%s", text)
	}
}

func TestDumpTextWarnings(t *testing.T) {
	world := fixtureWorld(t)
	var out bytes.Buffer
	if err := dumpTextSummary(&out, world, "warnings"); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Warnings") || !strings.Contains(text, "malformed-batch") {
		t.Fatalf("expected warnings output, got:\n%s", text)
	}
}

func TestVersionString(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, BuildDate
	t.Cleanup(func() {
		Version, Commit, BuildDate = originalVersion, originalCommit, originalDate
	})
	Version, Commit, BuildDate = "1.2.3", "abc123", "2026-06-15T00:00:00Z"
	got := VersionString()
	if !strings.Contains(got, "1.2.3") || !strings.Contains(got, "abc123") || !strings.Contains(got, "2026-06-15T00:00:00Z") {
		t.Fatalf("expected version metadata, got %q", got)
	}
}

func TestDumpDiagnosticsJSON(t *testing.T) {
	world := fixtureWorld(t)
	var out bytes.Buffer
	err := dumpDiagnosticsJSON(&out, runConfig{
		TasksDir:        "tasks",
		TeamsDir:        "teams",
		MaxFileSize:     claude.DefaultMaxFileSize,
		RefreshInterval: time.Second.String(),
		Focus:           "all",
		Shell:           true,
		LogLevel:        "info",
	}, world)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["schema_version"] != schema.CurrentSchemaVersion {
		t.Fatalf("expected schema version, got %#v", decoded["schema_version"])
	}
	if decoded["tasks"].(float64) == 0 {
		t.Fatalf("expected diagnostics task count, got %s", out.String())
	}
}

func TestRedactWorldHidesLocalPaths(t *testing.T) {
	world := fixtureWorld(t)
	config := runConfig{
		TasksDir: filepath.Join("..", "testdata", "claude", "tasks"),
		TeamsDir: filepath.Join("..", "testdata", "claude", "missing-teams"),
	}
	var originalActiveFile string
	for _, task := range world.Tasks {
		if len(task.ActiveFiles) > 0 {
			originalActiveFile = task.ActiveFiles[0].Path
			break
		}
	}
	redacted := redactWorld(world, config)
	for _, task := range redacted.Tasks {
		if strings.Contains(task.SourcePath, "testdata") {
			t.Fatalf("expected redacted source path, got %q", task.SourcePath)
		}
		for _, file := range task.ActiveFiles {
			if strings.Contains(file.Path, "/") && !strings.HasPrefix(file.Path, "<redacted>") {
				t.Fatalf("expected redacted active file, got %q", file.Path)
			}
		}
	}
	if originalActiveFile != "" {
		for _, task := range world.Tasks {
			for _, file := range task.ActiveFiles {
				if strings.HasPrefix(file.Path, "<redacted>") {
					t.Fatalf("redaction mutated original world active file path: %#v", world.Tasks)
				}
			}
		}
	}
	for _, warning := range redacted.Warnings {
		if strings.Contains(warning.SourcePath, "testdata") {
			t.Fatalf("expected redacted warning path, got %q", warning.SourcePath)
		}
	}
}

func TestDoctorReportMarksRequiredTasksDirFailure(t *testing.T) {
	world := fixtureWorld(t)
	report := buildDoctorReport(runConfig{
		TasksDir:    filepath.Join(t.TempDir(), "missing"),
		TeamsDir:    filepath.Join(t.TempDir(), "optional-missing"),
		MaxFileSize: claude.DefaultMaxFileSize,
	}, world)
	if !doctorHasFailure(report) {
		t.Fatalf("expected doctor failure for missing required tasks dir")
	}
}

func TestDoctorTextIncludesChecks(t *testing.T) {
	world := fixtureWorld(t)
	report := buildDoctorReport(runConfig{
		TasksDir:    filepath.Join("..", "testdata", "claude", "tasks"),
		TeamsDir:    filepath.Join("..", "testdata", "claude", "missing-teams"),
		MaxFileSize: claude.DefaultMaxFileSize,
	}, world)
	var out bytes.Buffer
	if err := dumpDoctorText(&out, report); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Agent Observer Doctor") || !strings.Contains(text, "tasks_dir") {
		t.Fatalf("expected doctor output, got:\n%s", text)
	}
}

func TestRedactDoctorReportPreservesCheckStatus(t *testing.T) {
	world := fixtureWorld(t)
	config := runConfig{
		TasksDir:    filepath.Join("..", "testdata", "claude", "tasks"),
		TeamsDir:    filepath.Join("..", "testdata", "claude", "missing-teams"),
		MaxFileSize: claude.DefaultMaxFileSize,
	}
	report := buildDoctorReport(config, world)
	if doctorHasFailure(report) {
		t.Fatalf("expected raw fixture doctor report to pass required checks")
	}
	redacted := redactDoctorReport(report, config)
	if doctorHasFailure(redacted) {
		t.Fatalf("expected redaction to preserve check status")
	}
	if strings.Contains(redacted.Config.TasksDir, "testdata") {
		t.Fatalf("expected redacted config path, got %#v", redacted.Config)
	}
}

func TestValidateOptionsRejectsBadRefreshInterval(t *testing.T) {
	opts := options{maxFileSize: claude.DefaultMaxFileSize, refreshInterval: time.Millisecond, focus: "all", logLevel: "info"}
	if err := validateOptions(&opts); err == nil || !strings.Contains(err.Error(), "--refresh-interval") {
		t.Fatalf("expected refresh interval validation error, got %v", err)
	}
}

func TestValidateOptionsRejectsConflictingShellFlags(t *testing.T) {
	opts := options{
		maxFileSize:     claude.DefaultMaxFileSize,
		refreshInterval: time.Second,
		focus:           "all",
		logLevel:        "info",
		telemetry:       "off",
		shell:           true,
		noShell:         true,
	}
	if err := validateOptions(&opts); err == nil || !strings.Contains(err.Error(), "SHELL") {
		t.Fatalf("expected shell conflict validation error, got %v", err)
	}
}

func TestShellDefaultsDisabled(t *testing.T) {
	opts := options{}
	if shellEnabled(opts) {
		t.Fatalf("expected shell to be disabled by default")
	}
	opts.shell = true
	if !shellEnabled(opts) {
		t.Fatalf("expected explicit shell opt-in to enable shell")
	}
	opts.noShell = true
	if shellEnabled(opts) {
		t.Fatalf("expected no-shell to disable shell")
	}
}

func TestValidateOptionsRejectsInvalidTelemetryEndpoint(t *testing.T) {
	opts := options{
		maxFileSize:     claude.DefaultMaxFileSize,
		refreshInterval: time.Second,
		focus:           "all",
		logLevel:        "info",
		telemetry:       "on",
		telemetryURL:    "file:///tmp/agent-observer",
	}
	if err := validateOptions(&opts); err == nil || !strings.Contains(err.Error(), "telemetry-endpoint") {
		t.Fatalf("expected telemetry endpoint validation error, got %v", err)
	}
}

func TestTelemetryClientIsOptIn(t *testing.T) {
	var calls int
	world := fixtureWorld(t)
	event := buildTelemetryEvent("app.start", options{command: commandWatch}, world, "")
	if err := newTelemetryClient(options{telemetry: "off", telemetryURL: "https://telemetry.example.invalid/events"}).Track(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("expected telemetry off to avoid network calls, got %d", calls)
	}
	client := httpTelemetryClient{
		endpoint: "https://telemetry.example.invalid/events",
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Status:     "204 No Content",
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	if err := client.Track(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one telemetry call, got %d", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestTelemetryEventDoesNotIncludeSensitiveFields(t *testing.T) {
	world := fixtureWorld(t)
	payload, err := json.Marshal(buildTelemetryEvent("scan.summary", options{command: commandWatch}, world, ""))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"Implement dashboard cockpit", "internal/tui/model.go", "testdata/claude/tasks"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("telemetry payload contains sensitive data %q: %s", forbidden, text)
		}
	}
}

func TestSetupLoggerWritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-observer.log")
	logger, cleanup, err := setupLogger(path, "debug")
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("test message", slog.String("key", "value"))
	cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test message") || !strings.Contains(string(data), "key=value") {
		t.Fatalf("expected structured log data, got %q", string(data))
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := Run(t.Context(), []string{"unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got %v", err)
	}
}

func TestRunRejectsInvalidRefreshInterval(t *testing.T) {
	err := Run(t.Context(), []string{"--refresh-interval", "10ms", "--dump-text"})
	if err == nil || !strings.Contains(err.Error(), "--refresh-interval") {
		t.Fatalf("expected refresh interval error, got %v", err)
	}
}

func TestRunParsesFlagsAfterWatchCommand(t *testing.T) {
	err := Run(t.Context(), []string{"watch", "--focus", "invalid"})
	if err == nil || !strings.Contains(err.Error(), "invalid --focus") {
		t.Fatalf("expected invalid focus error, got %v", err)
	}
}

func fixtureWorld(t *testing.T) schema.WorldSnapshot {
	t.Helper()
	tasks := filepath.Join("..", "testdata", "claude", "tasks")
	teams := filepath.Join("..", "testdata", "claude", "missing-teams")
	return claude.NewScanner(tasks, teams, claude.DefaultMaxFileSize).Scan()
}
