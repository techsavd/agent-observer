// Package providers builds the set of source adapters for a run.
package providers

import (
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/internal/providers/claude"
)

type Config struct {
	Claude claude.Config
}

// Build returns the adapters enabled for this run, in stable order.
func Build(cfg Config) []source.Adapter {
	return []source.Adapter{
		claude.New(cfg.Claude),
	}
}
