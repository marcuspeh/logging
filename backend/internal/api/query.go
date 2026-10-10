package api

import (
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
	Offset  int
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

// TailSource supplies events that have been accepted by the writer but
// not yet sealed into a Parquet file. *parquet.Writer satisfies it.
type TailSource interface {
	Tail() []model.LogEvent
}

// Engine runs queries across an IndexLoader.
type Engine struct {
	loader *IndexLoader
	logger *slog.Logger
	tail   TailSource
}

// NewEngine constructs an Engine bound to a loader. An optional
// TailSource may be supplied so events that are not yet sealed into a
// Parquet file remain queryable. The variadic keeps existing call sites
// (which pass only loader+logger) compiling unchanged.
func NewEngine(loader *IndexLoader, logger *slog.Logger, tail ...TailSource) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	e := &Engine{loader: loader, logger: logger}
	if len(tail) > 0 {
		e.tail = tail[0]
	}
	return e
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
	// Not-yet-sealed events live only in the writer's tail buffer, so the
	// sealed-file index alone would miss the most recent logs.
	var tailRows []model.LogEvent
	if e.tail != nil {
		tailRows = e.tail.Tail()
	}
	if len(entries) == 0 && len(tailRows) == 0 {
		return Response{Count: 0, Results: []ResultRow{}}, nil
	}

	cursors := make([]cursor, 0, len(entries))
	skipped := make([]string, 0)
	for _, ent := range entries {
		rows, err := scanParquetRows(ent.Path, q)
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

	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	// Collect every row that matches the per-row predicates so the
	// reported Count is the true total (clients need it to drive
	// pagination UI). The bounded heap was a micro-optimisation that
	// got in the way of accurate pagination; rows are already bounded
	// by the parquet file scan.
	var all []model.LogEvent
	for _, c := range cursors {
		for _, r := range c.rows {
			if !rowMatches(r, q) {
				continue
			}
			all = append(all, r)
		}
	}
	for _, r := range tailRows {
		if !rowMatches(r, q) {
			continue
		}
		all = append(all, r)
	}

	if q.Order == OrderAsc {
		sort.Slice(all, func(i, j int) bool { return all[i].Timestamp.Before(all[j].Timestamp) })
	} else {
		sort.Slice(all, func(i, j int) bool { return all[i].Timestamp.After(all[j].Timestamp) })
	}
	if offset > len(all) {
		offset = len(all)
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	page := all[offset:end]

	resp := Response{Count: len(all), Results: make([]ResultRow, 0, len(page))}
	for _, ev := range page {
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

// scanParquetRows returns only rows of a file matching q. It opens the
// file once and prunes row groups using the timestamp column's index
// statistics, so a narrow time window decodes only the row groups that
// can contain matches instead of every row in the file.
func scanParquetRows(path string, q Query) ([]model.LogEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	pf, err := parquetgo.OpenFile(f, st.Size())
	if err != nil {
		return nil, wrapScanErr(path, err)
	}

	groups := pf.RowGroups()
	selected := make([]parquetgo.RowGroup, 0, len(groups))
	for _, g := range groups {
		if rowGroupOverlaps(g, q) {
			selected = append(selected, g)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}

	// A RowGroup is not an io.ReaderAt, so read it via the row-group
	// reader rather than parquetgo.Read (which needs ReaderAt+size).
	rr := parquetgo.NewGenericRowGroupReader[model.LogEvent](parquetgo.MultiRowGroup(selected...))
	defer rr.Close()

	total := int(rr.NumRows())
	if total <= 0 {
		return nil, nil
	}
	buf := make([]model.LogEvent, total)
	n := 0
	for n < total {
		read, rerr := rr.Read(buf[n:])
		n += read
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return nil, wrapScanErr(path, rerr)
		}
	}

	out := make([]model.LogEvent, 0, n)
	for _, r := range buf[:n] {
		if rowMatches(r, q) {
			out = append(out, r)
		}
	}
	return out, nil
}

func wrapScanErr(path string, err error) error {
	if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "magic") {
		return fmt.Errorf("%w: %v", parquet.ErrFileUnreadable, err)
	}
	return fmt.Errorf("read %s: %w", path, err)
}

// rowGroupOverlaps reports whether a row group could hold a row inside
// q's time window, using the timestamp leaf column's index statistics.
// Absent statistics mean "might match", so rows are never wrongly dropped.
func rowGroupOverlaps(g parquetgo.RowGroup, q Query) bool {
	if q.From.IsZero() && q.To.IsZero() {
		return true
	}
	chunks := g.ColumnChunks()
	if len(chunks) == 0 {
		return true
	}
	// Timestamp is the first field of LogEvent, so it is leaf column 0.
	ci, err := chunks[0].ColumnIndex()
	if err != nil || ci == nil || ci.NumPages() == 0 {
		return true
	}
	// LogEvent maps time.Time to parquet Timestamp(Nanosecond), so the
	// physical values are nanoseconds since the Unix epoch.
	minV, maxV := ci.MinValue(0), ci.MaxValue(0)
	if minV.IsNull() || maxV.IsNull() {
		return true
	}
	if !q.From.IsZero() && maxV.Int64() < q.From.UnixNano() {
		return false
	}
	if !q.To.IsZero() && minV.Int64() > q.To.UnixNano() {
		return false
	}
	return true
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
