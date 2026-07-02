// Package claude adapts local Claude Code state to the source.Adapter contract.
package claude

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	claudescan "github.com/techsavd/agent-observer/internal/claude"
)

const ProviderName = "claude"

type Adapter struct {
	claudeDir string
	scanner   *claudescan.Scanner
}

type Config struct {
	ClaudeDir   string
	TasksDir    string
	TeamsDir    string
	MaxFileSize int64
}

func New(cfg Config) *Adapter {
	claudeDir := cfg.ClaudeDir
	if claudeDir == "" {
		home, _ := os.UserHomeDir()
		claudeDir = filepath.Join(home, ".claude")
	}
	tasksDir := cfg.TasksDir
	if tasksDir == "" {
		tasksDir = filepath.Join(claudeDir, "tasks")
	}
	teamsDir := cfg.TeamsDir
	if teamsDir == "" {
		teamsDir = filepath.Join(claudeDir, "teams")
	}
	return &Adapter{
		claudeDir: claudeDir,
		scanner:   claudescan.NewScanner(tasksDir, teamsDir, cfg.MaxFileSize),
	}
}

func (a *Adapter) Name() string { return ProviderName }

func (a *Adapter) Available() bool {
	if _, err := os.Stat(a.claudeDir); err == nil {
		return true
	}
	_, err := exec.LookPath("claude")
	return err == nil
}

func (a *Adapter) WatchPaths() []string {
	return []string{a.scanner.TasksDir, a.scanner.TeamsDir}
}

func (a *Adapter) Snapshot(ctx context.Context) source.ProviderSnapshot {
	world := a.scanner.ScanContext(ctx)
	cliPath, _ := exec.LookPath("claude")
	return source.ProviderSnapshot{
		Provider: ProviderName,
		Info: schema.ProviderInfo{
			Name:      ProviderName,
			Available: a.Available(),
			CLIPath:   cliPath,
		},
		Sessions: map[string]schema.SessionSnapshot{},
		Tasks:    world.Tasks,
		Batches:  world.Batches,
		Warnings: world.Warnings,
		Stats:    world.Stats,
	}
}
