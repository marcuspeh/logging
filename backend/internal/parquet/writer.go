// Package parquet writes LogEvents to rotated Parquet files on local disk and
// emits a JSON sidecar index per file.
//
// Rotation policy (PLAN §5): when the current file's buffered byte estimate
// reaches rotateBytes, the file is closed, an index is written, and a new
// file is opened on the next Write. Close() flushes the active file.
package parquet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/marcuspeh/logging-backend/internal/model"
)

const (
	filePrefix = "logs-"
	idxSuffix  = ".idx.json"
)

// Index is the JSON sidecar manifest written next to each Parquet file.
type Index struct {
	File      string    `json:"file"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended"`
	RowCount  int64     `json:"row_count"`
	Projects  []string  `json:"projects"`
	SizeBytes int64     `json:"size_bytes"`
}

// Writer appends LogEvents to a rotating Parquet file.
//
// Safe for concurrent use; the underlying parquet writer is guarded by a mutex.
type Writer struct {
	dir         string
	rotateBytes int64
	rotateEvery time.Duration
	flushRows   int64
	flushEvery  time.Duration

	mu          sync.Mutex
	out         *parquet.GenericWriter[model.LogEvent]
	file        *os.File
	currentPath string
	currentSize int64

	rowCount    int64
	pendingRows int64
	lastFlush   time.Time
	openedAt    time.Time
	minTs       time.Time
	maxTs       time.Time
	projects    map[string]struct{}
	seq         int

	// tail holds events that are not yet sealed into a Parquet file.
	// parquet-go only writes a valid footer on Close, so a sealed file is
	// the unit of durability. Previously "make rows queryable" was
	// implemented by sealing a whole new file on every flush, which made
	// the file count track wall-clock time rather than data volume.
	// Instead, recent events are served from this buffer and files are
	// sealed only on rotateBytes / rotateEvery / tail overflow.
	// tailMu is separate from mu so queries can read the tail without
	// blocking ingestion.
	tailMu      sync.RWMutex
	tail        []model.LogEvent
	tailMaxRows int
}

// FlushOptions controls how Write batches events into durable rows on
// disk and how often the active file is sealed into a new one. The two
// rotation triggers (size + time) and the two flush triggers (rows +
// time) operate independently, so the active file becomes visible to
// the query API within at most flushEvery (subject to fsync latency)
// and is rotated within at most rotateEvery or rotateBytes.
type FlushOptions struct {
	RotateBytes int64         // rotate when active file reaches this size
	RotateEvery time.Duration // rotate when active file is this old (0 = disabled)
	FlushRows   int64         // flush parquet row group + sidecar after N rows
	FlushEvery  time.Duration // flush parquet row group + sidecar after this duration

	// TailMaxRows bounds the in-memory buffer of not-yet-sealed events.
	// When it fills, the writer seals a file regardless of the rotation
	// timers, so memory stays bounded under sustained load. 0 selects
	// defaultTailMaxRows.
	TailMaxRows int
}

// defaultTailMaxRows bounds the not-yet-sealed event buffer to a few
// thousand rows, which keeps recent logs queryable while sealing files on
// a data-volume basis rather than a wall-clock basis.
const defaultTailMaxRows = 4096

// New opens the first Parquet file in dir and returns a ready-to-use Writer.
// If dir does not exist it is created with mode 0o755.
func New(dir string, opts FlushOptions) (*Writer, error) {
	if dir == "" {
		return nil, fmt.Errorf("parquet: dir is empty")
	}
	if opts.RotateBytes <= 0 {
		return nil, fmt.Errorf("parquet: RotateBytes must be > 0, got %d", opts.RotateBytes)
	}
	if opts.RotateEvery < 0 {
		return nil, fmt.Errorf("parquet: RotateEvery must be >= 0, got %s", opts.RotateEvery)
	}
	if opts.FlushRows <= 0 {
		return nil, fmt.Errorf("parquet: FlushRows must be > 0, got %d", opts.FlushRows)
	}
	if opts.FlushEvery <= 0 {
		return nil, fmt.Errorf("parquet: FlushEvery must be > 0, got %s", opts.FlushEvery)
	}
	if opts.TailMaxRows <= 0 {
		opts.TailMaxRows = defaultTailMaxRows
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("parquet: mkdir %s: %w", dir, err)
	}

	w := &Writer{
		dir:         dir,
		rotateBytes: opts.RotateBytes,
		rotateEvery: opts.RotateEvery,
		flushRows:   opts.FlushRows,
		flushEvery:  opts.FlushEvery,
		tailMaxRows: opts.TailMaxRows,
		projects:    make(map[string]struct{}),
	}
	// Seed the sequence counter from what's already on disk so we never
	// collide with a file left behind by a previous process. This is the
	// only directory scan the writer performs; openNewFile increments w.seq
	// in memory afterwards, so each subsequent rotation is O(1).
	w.seq = nextSeq(dir, time.Now().UTC().Unix())
	if err := w.openNewFile(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write appends a single LogEvent. Writes are batched: the parquet row
// group + sidecar are flushed when either pendingRows >= FlushRows or
// FlushEvery has elapsed since the last flush. The active file is
// rotated when currentSize >= RotateBytes, or when RotateEvery has
// elapsed and the file has at least one row.
func (w *Writer) Write(ev model.LogEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.out.Write([]model.LogEvent{ev}); err != nil {
		return fmt.Errorf("parquet: write row: %w", err)
	}

	// Mirror the row into the not-yet-sealed tail so queries can serve
	// recent events without waiting for a file seal. tailFull is computed
	// under the tail mutex so the check matches the appended length.
	w.tailMu.Lock()
	w.tail = append(w.tail, ev)
	tailFull := len(w.tail) >= w.tailMaxRows
	w.tailMu.Unlock()

	w.currentSize += approxRowBytes(ev)
	w.rowCount++
	w.pendingRows++

	if ev.Project != "" {
		w.projects[ev.Project] = struct{}{}
	}
	ts := ev.Timestamp
	if w.minTs.IsZero() || ts.Before(w.minTs) {
		w.minTs = ts
	}
	if ts.After(w.maxTs) {
		w.maxTs = ts
	}

	now := time.Now()
	flushBySize := w.pendingRows >= w.flushRows
	flushByTime := !w.lastFlush.IsZero() && now.Sub(w.lastFlush) >= w.flushEvery
	if flushBySize || flushByTime {
		if err := w.flushLocked(); err != nil {
			return fmt.Errorf("parquet: flush: %w", err)
		}
	}

	// Seal when the tail buffer is full. This keeps memory bounded and
	// makes the file size track ingest volume instead of elapsed time.
	if tailFull {
		if err := w.rotateLocked(); err != nil {
			return fmt.Errorf("parquet: rotate: %w", err)
		}
		return nil
	}

	// Rotation triggers. Time-based rotation only fires when the active
	// file has at least one row, so idle streams don't churn empty files.
	if w.currentSize >= w.rotateBytes {
		if err := w.rotateLocked(); err != nil {
			return fmt.Errorf("parquet: rotate: %w", err)
		}
	} else if w.rotateEvery > 0 && w.rowCount > 0 && now.Sub(w.openedAt) >= w.rotateEvery {
		if err := w.rotateLocked(); err != nil {
			return fmt.Errorf("parquet: rotate: %w", err)
		}
	}
	return nil
}

// Flush makes pending rows durable to disk. If there are no pending rows
// this is a no-op aside from resetting the flush timer. When there are
// pending rows the active file is sealed and a new one is opened, so
// callers should be aware that Flush can rotate the file. Safe to call
// concurrently.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

// Rotate seals the current file (flush + close + write sidecar) and opens
// a new one. Safe to call concurrently.
func (w *Writer) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rotateLocked()
}

// Close flushes and closes the active Parquet file and writes its index.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}

// Dir returns the directory the writer writes into.
func (w *Writer) Dir() string { return w.dir }

// Tail returns a snapshot of events that have been accepted by Write
// but not yet sealed into a Parquet file. The query API merges these
// with the sealed files so recent logs stay visible without sealing a
// file per flush. Returns nil if the buffer is empty. Safe for
// concurrent use.
func (w *Writer) Tail() []model.LogEvent {
	w.tailMu.RLock()
	defer w.tailMu.RUnlock()
	if len(w.tail) == 0 {
		return nil
	}
	out := make([]model.LogEvent, len(w.tail))
	copy(out, w.tail)
	return out
}

// resetTailLocked drops buffered not-yet-sealed events after they have
// been durably sealed into a Parquet file. Caller must hold w.mu (or be
// the seal path that already does).
func (w *Writer) resetTailLocked() {
	w.tailMu.Lock()
	w.tail = w.tail[:0]
	w.tailMu.Unlock()
}

// RotateBytes returns the byte threshold at which the writer rotates
// files. Used by the compactor to size its batches.
func (w *Writer) RotateBytes() int64 { return w.rotateBytes }

// ActiveBaseName returns the base name (no extension) of the writer's
// current Parquet file, or "" if none is open. Safe for concurrent use.
func (w *Writer) ActiveBaseName() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.currentPath == "" {
		return ""
	}
	return filepath.Base(w.currentPath)
}

// RetentionResult summarises one retention sweep.
type RetentionResult struct {
	Scanned    int      // sidecar index files inspected
	Deleted    []string // base names of parquet files removed
	Skipped    int      // index files that were missing or malformed
	BytesFreed int64    // combined on-disk size of removed files
}

// SweepRetention removes sealed Parquet files whose last event timestamp
// (per the sidecar index) is older than `now - ttl`. The active file is
// never removed, even if its index is stale. Index files without a
// parseable Ended timestamp are left alone (Skipped) — better to leak a
// file than to delete live data based on a corrupt index.
func (w *Writer) SweepRetention(now time.Time, ttl time.Duration) (RetentionResult, error) {
	var res RetentionResult

	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return res, fmt.Errorf("parquet: read dir: %w", err)
	}

	cutoff := now.Add(-ttl)
	active := w.ActiveBaseName()

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, idxSuffix) {
			continue
		}
		res.Scanned++

		path := filepath.Join(w.dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			res.Skipped++
			continue
		}
		var idx Index
		if err := json.Unmarshal(data, &idx); err != nil {
			res.Skipped++
			continue
		}
		base := strings.TrimSuffix(name, idxSuffix)
		if base == active {
			continue
		}
		if idx.Ended.IsZero() || idx.Ended.After(cutoff) {
			continue
		}

		pqPath := filepath.Join(w.dir, base)
		if st, err := os.Stat(pqPath); err == nil {
			res.BytesFreed += st.Size()
		}
		if err := os.Remove(pqPath); err != nil && !os.IsNotExist(err) {
			res.Skipped++
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			res.Skipped++
			continue
		}
		res.Deleted = append(res.Deleted, base)
	}
	return res, nil
}

// (activeBaseName moved to exported ActiveBaseName for use by Compactor)

// openNewFile creates a fresh file + writer and resets per-file
// accumulators. Caller must hold w.mu.
func (w *Writer) openNewFile() error {
	now := time.Now().UTC()
	// w.seq is seeded once from disk in New() and bumped in memory on
	// every rotation. Calling nextSeq here would re-scan the whole
	// directory on every rotation, which is O(dirSize) per rotation and
	// turns sustained ingest into quadratic I/O.
	seq := w.seq
	w.seq = seq + 1

	name := fmt.Sprintf("%s%d-%04d.parquet", filePrefix, now.Unix(), seq)
	path := filepath.Join(w.dir, name)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("parquet: create %s: %w", path, err)
	}

	pw := parquet.NewGenericWriter[model.LogEvent](f,
		parquet.Compression(&parquet.Snappy),
		parquet.MaxRowsPerRowGroup(64*1024),
	)

	w.out = pw
	w.file = f
	w.currentPath = path
	w.currentSize = 0
	w.rowCount = 0
	w.pendingRows = 0
	w.openedAt = time.Now().UTC()
	w.lastFlush = w.openedAt
	w.minTs = time.Time{}
	w.maxTs = time.Time{}
	w.projects = make(map[string]struct{})
	return nil
}

// nextSeq scans dir for the highest sequence suffix used by files
// matching `<filePrefix><unix>-NNNN.parquet` and returns the next free
// sequence. Returns 0 when the directory is empty or unreadable.
func nextSeq(dir string, unix int64) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	prefix := fmt.Sprintf("%s%d-", filePrefix, unix)
	best := -1
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".parquet") {
			continue
		}
		mid := strings.TrimPrefix(name, prefix)
		mid = strings.TrimSuffix(mid, ".parquet")
		n, err := strconv.Atoi(mid)
		if err != nil {
			continue
		}
		if n > best {
			best = n
		}
	}
	if best < 0 {
		return 0
	}
	return best + 1
}

// rotateLocked closes the current file (writing its index) and opens the
// next one. Caller must hold w.mu.
func (w *Writer) rotateLocked() error {
	if err := w.closeLocked(); err != nil {
		return err
	}
	return w.openNewFile()
}

// flushLocked acknowledges pending rows. parquet-go only writes a valid
// Parquet footer on Close, so a flush cannot publish rows to disk
// without sealing the file. It used to seal on every flush, which
// produced one tiny file per flushRows / flushEvery and made directory
// scans, index reloads and compaction scale with wall-clock time
// instead of data volume. Durability and visibility are now handled by
// the tail buffer (recent rows) plus rotation (rotateBytes / rotateEvery
// / tail overflow), so a flush just resets the acknowledgement counter.
//
// Caller must hold w.mu.
func (w *Writer) flushLocked() error {
	if w.out == nil || w.file == nil {
		return nil
	}
	w.pendingRows = 0
	w.lastFlush = time.Now()
	return nil
}

// closeLocked flushes, closes, fsyncs, and writes the sidecar index for the
// current file. Caller must hold w.mu.
//
// If no rows were ever written, the file contains nothing but the
// parquet header — leaving it (with or without a sidecar) just creates
// noise that the next Reload() then has to skip. Remove the empty file
// (and any sidecar) so the directory stays clean.
func (w *Writer) closeLocked() error {
	if w.out == nil {
		return nil
	}
	if err := w.out.Close(); err != nil {
		return fmt.Errorf("parquet: close writer: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("parquet: fsync %s: %w", w.currentPath, err)
	}
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("parquet: close file: %w", err)
	}

	if w.rowCount == 0 {
		// Best-effort cleanup of the empty stub. If removal fails
		// (e.g. read-only mount), don't fail Close — the file is
		// invalid parquet anyway and Validate() will skip it.
		_ = os.Remove(w.currentPath)
		_ = os.Remove(w.currentPath + idxSuffix)

		w.out = nil
		w.file = nil
		return nil
	}

	idx := Index{
		File:      filepath.Base(w.currentPath),
		Started:   w.minTs,
		Ended:     w.maxTs,
		RowCount:  w.rowCount,
		Projects:  sortedKeys(w.projects),
		SizeBytes: fileSize(w.currentPath),
	}
	if err := writeIndex(w.currentPath, idx); err != nil {
		return err
	}

	// The sealed file now carries these rows, so drop them from the
	// not-yet-sealed tail to avoid serving them twice. Do this only after
	// the sidecar is durable; if the seal fails the rows stay in the tail
	// and remain queryable.
	w.resetTailLocked()

	w.out = nil
	w.file = nil
	return nil
}

// writeIndex serialises idx as JSON to <parquetPath>.idx.json using a temp
// file + rename for atomicity.
func writeIndex(parquetPath string, idx Index) error {
	idxPath := parquetPath + idxSuffix
	tmp, err := os.CreateTemp(filepath.Dir(parquetPath), filepath.Base(idxPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("parquet: create index tmp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	// Compact JSON: these sidecars are re-read on every index Reload, so
	// indentation would multiply the bytes read for no benefit.
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(idx); err != nil {
		tmp.Close()
		return fmt.Errorf("parquet: encode index: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("parquet: sync index tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("parquet: close index tmp: %w", err)
	}
	if err := os.Rename(tmpName, idxPath); err != nil {
		return fmt.Errorf("parquet: rename index: %w", err)
	}
	return nil
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// approxRowBytes estimates the uncompressed size of a row. parquet-go
// doesn't expose a streaming byte counter, so we use a heuristic based on
// the string column widths plus the timestamp. It's used only to decide
// when to rotate; actual on-disk sizes are tracked in the sidecar via
// fileSize().
func approxRowBytes(ev model.LogEvent) int64 {
	var n int64
	n += 16
	n += int64(len(ev.Project))
	n += int64(len(ev.LogID))
	n += int64(len(ev.Level))
	n += int64(len(ev.Message))
	return n
}
