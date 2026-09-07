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

	mu          sync.Mutex
	out         *parquet.GenericWriter[model.LogEvent]
	file        *os.File
	currentPath string
	currentSize int64

	rowCount int64
	minTs    time.Time
	maxTs    time.Time
	projects map[string]struct{}
	seq      int
}

// New opens the first Parquet file in dir and returns a ready-to-use Writer.
// If dir does not exist it is created with mode 0o755.
func New(dir string, rotateBytes int64) (*Writer, error) {
	if dir == "" {
		return nil, fmt.Errorf("parquet: dir is empty")
	}
	if rotateBytes <= 0 {
		return nil, fmt.Errorf("parquet: rotateBytes must be > 0, got %d", rotateBytes)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("parquet: mkdir %s: %w", dir, err)
	}

	w := &Writer{
		dir:         dir,
		rotateBytes: rotateBytes,
		projects:    make(map[string]struct{}),
	}
	if err := w.openNewFile(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write appends a single LogEvent. If the resulting file would exceed
// rotateBytes, the current file is sealed and a new one is opened.
func (w *Writer) Write(ev model.LogEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.out.Write([]model.LogEvent{ev}); err != nil {
		return fmt.Errorf("parquet: write row: %w", err)
	}

	w.currentSize += approxRowBytes(ev)
	w.rowCount++

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

	if w.currentSize >= w.rotateBytes {
		if err := w.rotateLocked(); err != nil {
			return fmt.Errorf("parquet: rotate: %w", err)
		}
	}
	return nil
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

// openNewFile creates a fresh file + writer and resets per-file
// accumulators. Caller must hold w.mu.
func (w *Writer) openNewFile() error {
	now := time.Now().UTC()
	name := fmt.Sprintf("%s%d-%04d.parquet", filePrefix, now.Unix(), w.seq)
	path := filepath.Join(w.dir, name)

	f, err := os.Create(path)
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
	w.minTs = time.Time{}
	w.maxTs = time.Time{}
	w.projects = make(map[string]struct{})
	w.seq++
	return nil
}

// rotateLocked closes the current file (writing its index) and opens the
// next one. Caller must hold w.mu.
func (w *Writer) rotateLocked() error {
	if err := w.closeLocked(); err != nil {
		return err
	}
	return w.openNewFile()
}

// closeLocked flushes, closes, fsyncs, and writes the sidecar index for the
// current file. Caller must hold w.mu.
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

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
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
