// Package aggregate merges per-provider snapshots into one WorldSnapshot.
package aggregate

import (
	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
)

// Merge combines provider snapshots into a single world state. Session keys
// are always namespaced as "provider:id" so providers can never collide.
// Task and batch keys pass through unchanged (they are the primary keys the
// TUI and dump consumers already rely on) and only gain a "provider:" prefix
// when two providers produce the same key.
func Merge(snaps []source.ProviderSnapshot) schema.WorldSnapshot {
	world := schema.WorldSnapshot{
		SchemaVersion: schema.CurrentSchemaVersion,
		Providers:     map[string]schema.ProviderInfo{},
		Sessions:      map[string]schema.SessionSnapshot{},
		Tasks:         map[string]schema.TaskSnapshot{},
		Batches:       map[string]schema.BatchSnapshot{},
	}
	for _, snap := range snaps {
		info := snap.Info
		if info.Name == "" {
			info.Name = snap.Provider
		}
		world.Providers[snap.Provider] = info
		for id, session := range snap.Sessions {
			session.Provider = snap.Provider
			world.Sessions[snap.Provider+":"+id] = session
		}
		for id, task := range snap.Tasks {
			task.Provider = snap.Provider
			if _, exists := world.Tasks[id]; exists {
				id = snap.Provider + ":" + id
			}
			world.Tasks[id] = task
		}
		for id, batch := range snap.Batches {
			batch.Provider = snap.Provider
			if _, exists := world.Batches[id]; exists {
				id = snap.Provider + ":" + id
			}
			world.Batches[id] = batch
		}
		world.Warnings = append(world.Warnings, snap.Warnings...)
		addStats(&world.Stats, snap.Stats)
	}
	world.Stats.Warnings = len(world.Warnings)
	return world
}

func addStats(total *schema.ScanStats, stats schema.ScanStats) {
	total.FilesScanned += stats.FilesScanned
	total.CacheHits += stats.CacheHits
	total.SkippedOversize += stats.SkippedOversize
	total.SkippedSymlinks += stats.SkippedSymlinks
	total.PartialRetries += stats.PartialRetries
	total.LastDuration += stats.LastDuration
}
