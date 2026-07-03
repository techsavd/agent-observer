// Package providers builds the set of source adapters for a run.
package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/internal/providers/claude"
	"github.com/techsavd/agent-observer/internal/providers/codex"
	"github.com/techsavd/agent-observer/internal/providers/cursor"
	"github.com/techsavd/agent-observer/internal/providers/manifest"
)

type Config struct {
	// Enabled selects providers by name: claude, codex, cursor, plugins.
	// Empty means all.
	Enabled    []string
	Claude     claude.Config
	Codex      codex.Config
	Cursor     cursor.Config
	PluginsDir string
}

func DefaultPluginsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agent-observer", "providers")
}

// ParseEnabled validates a comma-separated --providers value.
func ParseEnabled(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return nil, nil
	}
	var enabled []string
	for _, name := range strings.Split(value, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		switch name {
		case "claude", "codex", "cursor", "plugins":
			enabled = append(enabled, name)
		case "":
		default:
			return nil, fmt.Errorf("unknown provider %q (valid: claude, codex, cursor, plugins)", name)
		}
	}
	return enabled, nil
}

// Build returns the adapters enabled for this run in stable order, plus any
// manifest files that failed to parse (keyed by path).
func Build(cfg Config) ([]source.Adapter, map[string]error) {
	enabled := func(name string) bool {
		if len(cfg.Enabled) == 0 {
			return true
		}
		for _, candidate := range cfg.Enabled {
			if candidate == name {
				return true
			}
		}
		return false
	}
	var adapters []source.Adapter
	if enabled("claude") {
		adapters = append(adapters, claude.New(cfg.Claude))
	}
	if enabled("codex") {
		adapters = append(adapters, codex.New(cfg.Codex))
	}
	if enabled("cursor") {
		adapters = append(adapters, cursor.New(cfg.Cursor))
	}
	var manifestErrs map[string]error
	if enabled("plugins") {
		pluginsDir := cfg.PluginsDir
		if pluginsDir == "" {
			pluginsDir = DefaultPluginsDir()
		}
		manifests, errs := manifest.Load(pluginsDir)
		manifestErrs = errs
		for _, m := range manifests {
			adapters = append(adapters, manifest.NewAdapter(m))
		}
	}
	return adapters, manifestErrs
}
