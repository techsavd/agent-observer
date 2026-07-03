// Package watch turns filesystem events into debounced per-provider dirty
// signals so the dashboard reacts to agent activity in milliseconds instead
// of waiting for the next poll.
//
// On darwin fsnotify rides kqueue: each watch costs a file descriptor, and a
// directory watch fires for entry create/rename/delete but NOT for writes to
// existing files. The strategy is therefore:
//
//   - watch provider directories (cheap, catches new sessions instantly), and
//   - watch a bounded LRU of "hot" files — recently modified transcripts —
//     individually, which is exactly where sub-100ms updates matter.
//
// A slower safety-net poll (owned by the engine) catches anything missed.
package watch

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	DefaultDebounce = 75 * time.Millisecond
	// DefaultMaxHotFiles bounds individual file watches (kqueue FDs).
	DefaultMaxHotFiles = 128
	// hotWindow is how recently a file must have changed to stay hot.
	hotWindow = 10 * time.Minute
)

// Dirty reports which providers have pending filesystem changes.
type Dirty struct {
	Providers map[string]bool
}

type Watcher struct {
	fs       *fsnotify.Watcher
	debounce time.Duration
	maxHot   int

	mu      sync.Mutex
	dirs    map[string]string    // watched dir -> provider
	hot     map[string]time.Time // watched file -> last event time
	hotProv map[string]string    // watched file -> provider
}

// Start watches the given provider directories and emits debounced dirty
// sets on out until ctx ends. targets maps provider name -> directories.
func Start(ctx context.Context, targets map[string][]string, debounce time.Duration, out chan<- Dirty) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if debounce <= 0 {
		debounce = DefaultDebounce
	}
	w := &Watcher{
		fs:       fsw,
		debounce: debounce,
		maxHot:   DefaultMaxHotFiles,
		dirs:     map[string]string{},
		hot:      map[string]time.Time{},
		hotProv:  map[string]string{},
	}
	for provider, dirs := range targets {
		for _, dir := range dirs {
			if dir == "" {
				continue
			}
			if err := fsw.Add(dir); err == nil {
				w.dirs[dir] = provider
			}
			// Missing dirs are fine: providers may not be installed. The
			// engine's safety poll covers dirs that appear later.
		}
	}
	go w.run(ctx, out)
	return w, nil
}

// MarkHot registers path for write-level watching, evicting the stalest hot
// file when the budget is exhausted. Call it for files a scan saw changing.
func (w *Watcher) MarkHot(provider, path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if _, ok := w.hot[path]; ok {
		w.hot[path] = now
		return
	}
	w.evictLocked(now)
	if len(w.hot) >= w.maxHot {
		w.evictOldestLocked()
	}
	if err := w.fs.Add(path); err != nil {
		return
	}
	w.hot[path] = now
	w.hotProv[path] = provider
}

func (w *Watcher) evictLocked(now time.Time) {
	for path, seen := range w.hot {
		if now.Sub(seen) > hotWindow {
			w.removeHotLocked(path)
		}
	}
}

func (w *Watcher) evictOldestLocked() {
	type entry struct {
		path string
		seen time.Time
	}
	entries := make([]entry, 0, len(w.hot))
	for path, seen := range w.hot {
		entries = append(entries, entry{path, seen})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].seen.Before(entries[j].seen) })
	// Free a small batch so bursts do not evict one-by-one.
	for i := 0; i < len(entries) && len(w.hot) >= w.maxHot; i++ {
		w.removeHotLocked(entries[i].path)
	}
}

func (w *Watcher) removeHotLocked(path string) {
	_ = w.fs.Remove(path)
	delete(w.hot, path)
	delete(w.hotProv, path)
}

// HotCount reports how many files are individually watched (for stats/tests).
func (w *Watcher) HotCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.hot)
}

func (w *Watcher) Close() error { return w.fs.Close() }

func (w *Watcher) run(ctx context.Context, out chan<- Dirty) {
	pending := map[string]bool{}
	var timer *time.Timer
	var timerC <-chan time.Time
	flush := func() {
		if len(pending) == 0 {
			return
		}
		dirty := Dirty{Providers: pending}
		pending = map[string]bool{}
		select {
		case out <- dirty:
		case <-ctx.Done():
		}
	}
	for {
		select {
		case <-ctx.Done():
			_ = w.fs.Close()
			return
		case event, ok := <-w.fs.Events:
			if !ok {
				flush()
				return
			}
			provider := w.providerFor(event.Name)
			if provider == "" {
				continue
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				w.mu.Lock()
				if _, hot := w.hot[event.Name]; hot {
					w.removeHotLocked(event.Name)
				}
				w.mu.Unlock()
			}
			pending[provider] = true
			if timer == nil {
				timer = time.NewTimer(w.debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
		case <-timerC:
			flush()
			timer = nil
			timerC = nil
		case _, ok := <-w.fs.Errors:
			if !ok {
				flush()
				return
			}
		}
	}
}

func (w *Watcher) providerFor(name string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if provider, ok := w.hotProv[name]; ok {
		return provider
	}
	dir := filepath.Dir(name)
	if provider, ok := w.dirs[dir]; ok {
		return provider
	}
	// Events can arrive for entries below a watched dir on some platforms;
	// fall back to the longest watched prefix.
	best := ""
	provider := ""
	for watched, p := range w.dirs {
		if strings.HasPrefix(name, watched+string(os.PathSeparator)) && len(watched) > len(best) {
			best = watched
			provider = p
		}
	}
	return provider
}
