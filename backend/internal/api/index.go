// Package api exposes the HTTP query endpoints backed by the Parquet index
// and files (PLAN §6).
//
// The package is split into three files:
//
//   - index.go  : IndexLoader — caches *.idx.json sidecars from ParquetDir.
//   - query.go  : QueryEngine — runs the filter over surviving Parquet files.
//   - server.go : HTTP handlers, router wiring, and Server.Run().
package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcuspeh/logging-backend/internal/parquet"
)

// indexSuffix matches what the writer uses (parquet.idxSuffix).
const indexSuffix = ".parquet.idx.json"

// IndexEntry pairs a parsed Index with the absolute path of its Parquet file.
type IndexEntry struct {
	Index parquet.Index
	Path  string
}

// IndexLoader scans a directory for *.parquet.idx.json sidecars, parses
// them, and serves them from an in-memory cache. Reload() can be called
// any time to refresh.
type IndexLoader struct {
	dir string

	mu      sync.RWMutex
	entries []IndexEntry
}

// NewIndexLoader creates a loader and performs an initial Load.
func NewIndexLoader(dir string) (*IndexLoader, error) {
	l := &IndexLoader{dir: dir}
	if err := l.Reload(); err != nil {
		return nil, err
	}
	return l, nil
}

// Reload re-scans the directory and rebuilds the cache. Files that have no
// sidecar yet (still-open writer) are skipped — they will appear in the
// cache once the writer seals them.
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

	var entries []IndexEntry
	for _, idxPath := range matches {
		// Translate ".../logs-X-NNNN.parquet.idx.json" back to
		// ".../logs-X-NNNN.parquet".
		pqPath := strings.TrimSuffix(idxPath, ".idx.json")

		data, err := os.ReadFile(idxPath)
		if err != nil {
			return fmt.Errorf("api: read %s: %w", idxPath, err)
		}
		var idx parquet.Index
		if err := json.Unmarshal(data, &idx); err != nil {
			return fmt.Errorf("api: parse %s: %w", idxPath, err)
		}
		entries = append(entries, IndexEntry{Index: idx, Path: pqPath})
	}

	// Newest first so query results in default order (desc) come from
	// recent files cheaply.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Index.Started.After(entries[j].Index.Started)
	})

	l.mu.Lock()
	l.entries = entries
	l.mu.Unlock()
	return nil
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

// Filter narrows the cached entries by the given query predicates.
// Entries survive only if:
//
//   - their time range overlaps [from, to] (open-ended bounds are honoured),
//   - and project (if non-empty) is listed in the entry's project set.
//
// The slice returned is a fresh copy and may be reordered / truncated by the
// caller.
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

// contains is a tiny case-sensitive substring check; the project list is
// small (low cardinality) so a map is overkill.
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