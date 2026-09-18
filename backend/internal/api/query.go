package api

import (
	"container/heap"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	parquetgo "github.com/parquet-go/parquet-go"

	"github.com/marcuspeh/logging-backend/internal/model"
	"github.com/marcuspeh/logging-backend/internal/parquet"
)

// Query holds the parsed query parameters (PLAN §6).
//
// Either Project or LogID must be set (enforced by the handler).
type Query struct {
	Project string
	LogID   string
	Level   string
	From    time.Time
	To      time.Time
	Limit   int
	Order   Order
}

// Order is the result ordering by timestamp.
type Order int

const (
	OrderDesc Order = iota // newest first (default)
	OrderAsc               // oldest first
)

func (o Order) String() string {
	if o == OrderAsc {
		return "asc"
	}
	return "desc"
}

// ResultRow is the JSON shape returned to clients. Caller carries
// the SDK-resolved "file:LINE" of the call site, or "" when caller
// capture is disabled.
type ResultRow struct {
	Timestamp time.Time `json:"timestamp"`
	Project   string    `json:"project"`
	LogID     string    `json:"logid"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Caller    string    `json:"caller"`
}

// Response is the envelope around a list of ResultRows.
type Response struct {
	Count   int         `json:"count"`
	Results []ResultRow `json:"results"`
}

// rowHeap implements a min-heap on row timestamp, used for bounded K-way
// merge across multiple Parquet files.
type rowHeap struct {
	rows []model.LogEvent
	less func(a, b model.LogEvent) bool
}

func (h rowHeap) Len() int           { return len(h.rows) }
func (h rowHeap) Less(i, j int) bool { return h.less(h.rows[i], h.rows[j]) }
func (h rowHeap) Swap(i, j int)      { h.rows[i], h.rows[j] = h.rows[j], h.rows[i] }
func (h *rowHeap) Push(x interface{}) {
	h.rows = append(h.rows, x.(model.LogEvent))
}
func (h *rowHeap) Pop() interface{} {
	old := h.rows
	n := len(old)
	x := old[n-1]
	h.rows = old[:n-1]
	return x
}

// Engine runs queries across an IndexLoader.
type Engine struct {
	loader *IndexLoader
	logger *slog.Logger
}

// NewEngine constructs an Engine bound to a loader.
func NewEngine(loader *IndexLoader, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{loader: loader, logger: logger}
}

// Execute runs q, returning up to q.Limit rows. Either q.Project or q.LogID
// must be non-empty.
//
// Parquet files that fail to open or read are logged and skipped rather
// than aborting the whole query — a single truncated / mid-rotation file
// shouldn't block every other project from being searchable. After a
// skip we also reload the index in the background so the bad entry is
// dropped from future queries (the writer may not have finished flushing
// it yet).
func (e *Engine) Execute(q Query) (Response, error) {
	entries := e.loader.Filter(q)
	if len(entries) == 0 {
		return Response{Count: 0, Results: []ResultRow{}}, nil
	}

	cursors := make([]cursor, 0, len(entries))
	skipped := make([]string, 0)
	for _, ent := range entries {
		rows, err := readParquetRows(ent.Path)
		if err != nil {
			e.logger.Warn("skipping unreadable parquet file", "path", ent.Path, "err", err)
			skipped = append(skipped, ent.Path)
			continue
		}
		cursors = append(cursors, cursor{rows: rows})
	}
	if len(skipped) > 0 {
		// Refresh the index so the bad entries stop being served. A
		// writer still flushing them will re-add them once the file
		// is valid; the IndexLoader validator catches that case.
		go func() {
			if err := e.loader.Reload(); err != nil {
				e.logger.Warn("index reload after skip", "err", err)
			}
		}()
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	less := func(a, b model.LogEvent) bool { return a.Timestamp.Before(b.Timestamp) }
	if q.Order == OrderDesc {
		less = func(a, b model.LogEvent) bool { return a.Timestamp.After(b.Timestamp) }
	}

	merged := &rowHeap{less: less}
	for _, c := range cursors {
		for _, r := range c.rows {
			if !rowMatches(r, q) {
				continue
			}
			if merged.Len() < limit {
				heap.Push(merged, r)
			} else if less(r, merged.rows[0]) {
				heap.Pop(merged)
				heap.Push(merged, r)
			}
		}
	}

	out := make([]model.LogEvent, 0, merged.Len())
	for merged.Len() > 0 {
		out = append(out, heap.Pop(merged).(model.LogEvent))
	}
	if q.Order == OrderAsc {
		sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	} else {
		sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	}

	resp := Response{Count: len(out), Results: make([]ResultRow, 0, len(out))}
	for _, ev := range out {
		resp.Results = append(resp.Results, ResultRow{
			Timestamp: ev.Timestamp,
			Project:   ev.Project,
			LogID:     ev.LogID,
			Level:     ev.Level,
			Message:   ev.Message,
			Caller:    ev.Caller,
		})
	}
	return resp, nil
}

type cursor struct {
	rows []model.LogEvent
}

// readParquetRows decodes all rows from a Parquet file. parquet-go applies
// dictionary + page filtering where possible.
//
// A quick magic-bytes check runs first; if the file is truncated or
// hasn't been flushed yet, parquet-go would otherwise return the
// cryptic "EOF" / "magic header" error we'd otherwise propagate.
func readParquetRows(path string) ([]model.LogEvent, error) {
	if err := parquet.Validate(path); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	rows, err := parquetgo.Read[model.LogEvent](f, st.Size())
	if err != nil {
		// Convert any "EOF" / "magic" errors from parquet-go into our
		// sentinel so the caller can decide to skip rather than fail.
		if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "magic") {
			return nil, fmt.Errorf("%w: %v", parquet.ErrFileUnreadable, err)
		}
		return nil, err
	}
	return rows, nil
}

// rowMatches applies the per-row filters the index can't fully enforce.
// The index narrows to files that *mention* the project, but a single
// file can still contain rows for other projects, so Project is re-checked
// here.
func rowMatches(r model.LogEvent, q Query) bool {
	if q.Project != "" && r.Project != q.Project {
		return false
	}
	if q.LogID != "" && r.LogID != q.LogID {
		return false
	}
	if q.Level != "" && r.Level != q.Level {
		return false
	}
	if !q.From.IsZero() && r.Timestamp.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && r.Timestamp.After(q.To) {
		return false
	}
	return true
}
