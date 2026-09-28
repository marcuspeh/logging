// Package api exposes the HTTP query endpoints backed by the Parquet index
// and files (PLAN §6).
//
// Split into three files:
//   - index.go  : IndexLoader — caches *.idx.json sidecars.
//   - query.go  : QueryEngine — filter + scan across surviving Parquet files.
//   - server.go : HTTP handlers, router wiring, Server.Run().
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcuspeh/logging-backend/internal/parquet"
)

const indexSuffix = ".parquet.idx.json"

// IndexEntry pairs a parsed Index with the absolute path of its Parquet file.
type IndexEntry struct {
	Index parquet.Index
	Path  string
}

// IndexLoader scans a directory for *.parquet.idx.json sidecars, parses
// them, and serves them from an in-memory cache. Reload() refreshes it.
type IndexLoader struct {
	dir    string
	logger *slog.Logger

	mu      sync.RWMutex
	entries []IndexEntry

	// lastUnreadableWarn tracks the last time we logged a "skipping
	// unreadable parquet file" WARN for a given path, so the periodic
	// Reload() doesn't spam the log once every indexReloadInterval for
	// the same broken sidecar. A path is re-warned at most once every
	// unreadableWarnInterval. Entries are pruned on every Reload().
	lastUnreadableWarn     map[string]time.Time
	unreadableWarnInterval time.Duration
}

const defaultUnreadableWarnInterval = 5 * time.Minute

// NewIndexLoader creates a loader and performs an initial Load.
func NewIndexLoader(dir string, logger *slog.Logger) (*IndexLoader, error) {
	if logger == nil {
		logger = slog.Default()
	}
	l := &IndexLoader{
		dir:                    dir,
		logger:                 logger,
		lastUnreadableWarn:     make(map[string]time.Time),
		unreadableWarnInterval: defaultUnreadableWarnInterval,
	}
	if err := l.Reload(); err != nil {
		return nil, err
	}
	return l, nil
}

// Reload re-scans the directory and rebuilds the cache. Files without a
// sidecar (still-open writer) are skipped.
func (l *IndexLoader) Reload() error {
	if l.dir == "" {
		return fmt.Errorf("api: index loader dir is empty")
	}
	st, err := os.Stat(l.dir)
	if err != nil {
		return fmt.Errorf("api: stat %s: %w", l.dir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("api: %s is not a directory", l.dir)
	}

	matches, err := filepath.Glob(filepath.Join(l.dir, "*"+indexSuffix))
	if err != nil {
		return fmt.Errorf("api: glob index: %w", err)
	}

	// Track which paths we touched this pass so the rate-limit map
	// doesn't keep growing forever.
	seen := make(map[string]struct{}, len(matches))

	var entries []IndexEntry
	for _, idxPath := range matches {
		pqPath := strings.TrimSuffix(idxPath, ".idx.json")
		seen[pqPath] = struct{}{}

		data, err := os.ReadFile(idxPath)
		if err != nil {
			return fmt.Errorf("api: read %s: %w", idxPath, err)
		}
		var idx parquet.Index
		if err := json.Unmarshal(data, &idx); err != nil {
			return fmt.Errorf("api: parse %s: %w", idxPath, err)
		}
		// Skip sidecars whose parquet file is missing, zero-bytes, or
		// missing the PAR1 magic markers. The writer hadn't finished
		// flushing or the file got truncated; either way the query
		// engine would hit EOF trying to read it. The WARN is
		// throttled per-path so a single orphan sidecar can't spam
		// the log on every periodic Reload().
		if err := parquet.Validate(pqPath); err != nil {
			l.warnUnreadable(pqPath, err)
			continue
		}
		// Path validated — clear any prior throttle entry so the next
		// regression is loud.
		l.clearUnreadableWarn(pqPath)
		entries = append(entries, IndexEntry{Index: idx, Path: pqPath})
	}

	// Prune rate-limit entries for paths that no longer have a sidecar
	// (e.g. operator deleted the orphan file).
	l.pruneUnreadableWarn(seen)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Index.Started.After(entries[j].Index.Started)
	})

	l.mu.Lock()
	l.entries = entries
	l.mu.Unlock()
	return nil
}

// warnUnreadable logs the WARN for path at most once per
// unreadableWarnInterval. Suppressed calls still return silently so
// callers don't need to know about the throttle.
func (l *IndexLoader) warnUnreadable(path string, err error) {
	l.mu.Lock()
	last, ok := l.lastUnreadableWarn[path]
	now := time.Now()
	if ok && now.Sub(last) < l.unreadableWarnInterval {
		l.mu.Unlock()
		return
	}
	l.lastUnreadableWarn[path] = now
	l.mu.Unlock()
	l.logger.Warn("skipping unreadable parquet file", "path", path, "err", err)
}

func (l *IndexLoader) clearUnreadableWarn(path string) {
	l.mu.Lock()
	delete(l.lastUnreadableWarn, path)
	l.mu.Unlock()
}

func (l *IndexLoader) pruneUnreadableWarn(seen map[string]struct{}) {
	l.mu.Lock()
	for p := range l.lastUnreadableWarn {
		if _, ok := seen[p]; !ok {
			delete(l.lastUnreadableWarn, p)
		}
	}
	l.mu.Unlock()
}

// Entries returns a snapshot of the loaded entries.
func (l *IndexLoader) Entries() []IndexEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]IndexEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Count returns how many entries are currently loaded.
func (l *IndexLoader) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Filter narrows the cached entries by query predicates. Entries survive
// only if their time range overlaps [from, to] (open-ended bounds are
// honoured) and project (if non-empty) appears in the entry's project set.
func (l *IndexLoader) Filter(q Query) []IndexEntry {
	candidates := l.Entries()
	if candidates == nil {
		return nil
	}
	out := candidates[:0]
	for _, e := range candidates {
		if q.Project != "" && !contains(e.Index.Projects, q.Project) {
			continue
		}
		if !overlaps(e.Index, q.From, q.To) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// overlaps reports whether the index's [Started, Ended] interval intersects
// the requested [from, to]. A zero bound is treated as open-ended.
func overlaps(idx parquet.Index, from, to time.Time) bool {
	if !from.IsZero() && idx.Ended.Before(from) {
		return false
	}
	if !to.IsZero() && idx.Started.After(to) {
		return false
	}
	return true
}
