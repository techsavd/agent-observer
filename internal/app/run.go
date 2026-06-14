package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/internal/claude"
	"github.com/techsavd/agent-observer/internal/tui"
)

func Run(ctx context.Context, args []string) error {
	home, _ := os.UserHomeDir()
	tasksDir := filepath.Join(home, ".claude", "tasks")
	teamsDir := filepath.Join(home, ".claude", "teams")
	debug := false
	dumpJSON := false
	dumpText := false
	fs := flag.NewFlagSet("agent-observer", flag.ContinueOnError)
	fs.StringVar(&tasksDir, "tasks-dir", tasksDir, "Claude tasks directory")
	fs.StringVar(&teamsDir, "teams-dir", teamsDir, "Claude teams directory")
	fs.BoolVar(&debug, "debug", false, "show debug UI")
	fs.BoolVar(&dumpJSON, "dump-json", false, "dump snapshot JSON and exit")
	fs.BoolVar(&dumpText, "dump-text", false, "dump snapshot text and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	refresh := func(context.Context) schema.WorldSnapshot {
		return claude.Scan(tasksDir, teamsDir)
	}
	world := claude.Scan(tasksDir, teamsDir)
	if dumpJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(world)
	}
	if dumpText {
		fmt.Fprintf(os.Stdout, "Agent Observer Snapshot\nBatches: %d\nTasks: %d\nWarnings: %d\nRefresh: %s\n", len(world.Batches), len(world.Tasks), len(world.Warnings), world.Stats.LastDuration.Round(time.Millisecond))
		return nil
	}
	model := tui.New(world, debug, refresh)
	_, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
