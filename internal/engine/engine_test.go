package engine

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
	"github.com/techsavd/agent-observer/core/source"
	"github.com/techsavd/agent-observer/core/store"
)

type fakeAdapter struct {
	name  string
	calls atomic.Int64
}

func (f *fakeAdapter) Name() string         { return f.name }
func (f *fakeAdapter) Available() bool      { return true }
func (f *fakeAdapter) WatchPaths() []string { return nil }
func (f *fakeAdapter) Snapshot(context.Context) source.ProviderSnapshot {
	f.calls.Add(1)
	return source.ProviderSnapshot{
		Provider: f.name,
		Info:     schema.ProviderInfo{Name: f.name, Available: true},
		Sessions: map[string]schema.SessionSnapshot{
			"s": {ID: "s", Status: schema.SessionIdle},
		},
	}
}

func TestEngineDirtyScanOnlyRescansDirtyProviders(t *testing.T) {
	a := &fakeAdapter{name: "a"}
	b := &fakeAdapter{name: "b"}
	e := New([]source.Adapter{a, b}, store.NewMemoryStore(), slog.Default(), Config{PollInterval: time.Hour})
	ctx := context.Background()
	e.scan(ctx, nil, TriggerPoll, nil)
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatalf("full scan should hit all adapters: a=%d b=%d", a.calls.Load(), b.calls.Load())
	}
	e.scan(ctx, map[string]bool{"a": true}, TriggerWatch, nil)
	if a.calls.Load() != 2 {
		t.Fatalf("dirty provider not rescanned: a=%d", a.calls.Load())
	}
	if b.calls.Load() != 1 {
		t.Fatalf("clean provider rescanned: b=%d", b.calls.Load())
	}
	world := e.store.Snapshot()
	if len(world.Sessions) != 2 {
		t.Fatalf("cached clean snapshot lost: sessions=%d", len(world.Sessions))
	}
	if world.Stats.LastTrigger != TriggerWatch {
		t.Fatalf("trigger = %q", world.Stats.LastTrigger)
	}
}

func TestEngineRunNotifiesAndHonorsManualRefresh(t *testing.T) {
	a := &fakeAdapter{name: "a"}
	e := New([]source.Adapter{a}, store.NewMemoryStore(), slog.Default(), Config{PollInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notified := make(chan schema.WorldSnapshot, 4)
	go e.Run(ctx, func(w schema.WorldSnapshot) { notified <- w })
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("no initial scan notification")
	}
	e.RequestRefresh()
	select {
	case world := <-notified:
		if world.Stats.LastTrigger != TriggerManual {
			t.Fatalf("trigger = %q, want manual", world.Stats.LastTrigger)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no manual refresh notification")
	}
}
