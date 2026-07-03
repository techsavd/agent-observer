// Package engine schedules provider scans. It reacts to filesystem dirty
// signals, a safety-net poll, and manual refresh requests, rescanning only
// the providers that changed and pushing merged snapshots to the UI.
package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/techsavd/agent-observer/core/aggregate"
	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/store"
	"github.com/techsavd/agent-observer/core/watch"
)

const (
	TriggerWatch  = "watch"
	TriggerPoll   = "poll"
	TriggerManual = "manual"

	// hotMarkWindow decides which session files a scan nominates for
	// write-level watching.
	hotMarkWindow = 10 * time.Minute
)

type Config struct {
	PollInterval time.Duration
	WatchEnabled bool
	Debounce     time.Duration
}

type Engine struct {
	adapters  []source.Adapter
	store     *store.MemoryStore
	logger    *slog.Logger
	cfg       Config
	refreshCh chan string
	lastSnaps map[string]source.ProviderSnapshot
	watcher   *watch.Watcher
	watchOn   bool
}

func New(adapters []source.Adapter, memory *store.MemoryStore, logger *slog.Logger, cfg Config) *Engine {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	return &Engine{
		adapters:  adapters,
		store:     memory,
		logger:    logger,
		cfg:       cfg,
		refreshCh: make(chan string, 1),
		lastSnaps: map[string]source.ProviderSnapshot{},
	}
}

// RequestRefresh asks for a full rescan without blocking the caller.
func (e *Engine) RequestRefresh() {
	select {
	case e.refreshCh <- TriggerManual:
	default:
	}
}

// WatchActive reports whether filesystem watching is running.
func (e *Engine) WatchActive() bool { return e.watchOn }

// Run scans until ctx ends, calling notify after every scan. The first scan
// happens synchronously before Run returns control to the select loop, so
// callers can rely on the store being populated quickly.
func (e *Engine) Run(ctx context.Context, notify func(schema.WorldSnapshot)) {
	dirtyCh := make(chan watch.Dirty, 8)
	if e.cfg.WatchEnabled {
		targets := map[string][]string{}
		for _, adapter := range e.adapters {
			targets[adapter.Name()] = adapter.WatchPaths()
		}
		watcher, err := watch.Start(ctx, targets, e.cfg.Debounce, dirtyCh)
		if err != nil {
			e.logger.Warn("filesystem watch unavailable; falling back to polling", slog.String("error", err.Error()))
		} else {
			e.watcher = watcher
			e.watchOn = true
		}
	}
	e.scan(ctx, nil, TriggerPoll, notify)
	ticker := time.NewTicker(e.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case dirty := <-dirtyCh:
			e.scan(ctx, dirty.Providers, TriggerWatch, notify)
		case <-ticker.C:
			e.scan(ctx, nil, TriggerPoll, notify)
		case trigger := <-e.refreshCh:
			e.scan(ctx, nil, trigger, notify)
		}
	}
}

// scan refreshes the named providers (nil means all), reusing cached
// snapshots for clean ones, then merges, stores, and notifies.
func (e *Engine) scan(ctx context.Context, only map[string]bool, trigger string, notify func(schema.WorldSnapshot)) {
	start := time.Now()
	snaps := make([]source.ProviderSnapshot, 0, len(e.adapters))
	for _, adapter := range e.adapters {
		name := adapter.Name()
		if only != nil && !only[name] {
			if cached, ok := e.lastSnaps[name]; ok {
				snaps = append(snaps, cached)
				continue
			}
		}
		snap := adapter.Snapshot(ctx)
		e.lastSnaps[name] = snap
		e.markHot(snap)
		snaps = append(snaps, snap)
	}
	world := aggregate.Merge(snaps)
	world.Stats.LastTrigger = trigger
	world.Stats.LastDuration = time.Since(start)
	world = e.store.Replace(world)
	e.logger.Debug("scan complete",
		slog.String("trigger", trigger),
		slog.Int("sessions", len(world.Sessions)),
		slog.Int("tasks", len(world.Tasks)),
		slog.Int("warnings", len(world.Warnings)),
		slog.Duration("duration", world.Stats.LastDuration),
	)
	if notify != nil {
		notify(world)
	}
}

// markHot nominates recently active session files for write-level watching.
func (e *Engine) markHot(snap source.ProviderSnapshot) {
	if e.watcher == nil {
		return
	}
	cutoff := time.Now().Add(-hotMarkWindow)
	for _, session := range snap.Sessions {
		if session.SourcePath == "" || session.LastUpdated.Before(cutoff) {
			continue
		}
		e.watcher.MarkHot(snap.Provider, session.SourcePath)
	}
}
