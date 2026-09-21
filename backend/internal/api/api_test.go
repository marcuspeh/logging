package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcuspeh/logging-backend/internal/model"
	"github.com/marcuspeh/logging-backend/internal/parquet"
)

func writeFixture(t *testing.T, dir string, events []model.LogEvent, rotateBytes int64) {
	t.Helper()
	w, err := parquet.New(dir, parquet.FlushOptions{
		RotateBytes: rotateBytes,
		FlushRows:   1024,
		FlushEvery:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("parquet.New: %v", err)
	}
	for _, ev := range events {
		if err := w.Write(ev); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func makeEvent(t time.Time, project, logid, level, message string) model.LogEvent {
	return model.LogEvent{Timestamp: t, Project: project, LogID: logid, Level: level, Message: message}
}

func TestIndexLoaderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	events := []model.LogEvent{
		makeEvent(base, "billing", "req-1", "INFO", "ok"),
		makeEvent(base.Add(time.Second), "auth", "req-2", "ERROR", "boom"),
	}
	writeFixture(t, dir, events, 1<<20)

	loader, err := NewIndexLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewIndexLoader: %v", err)
	}
	if got := loader.Count(); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}
	entries := loader.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries len = %d, want 1", len(entries))
	}
	if entries[0].Index.RowCount != int64(len(events)) {
		t.Errorf("RowCount = %d, want %d", entries[0].Index.RowCount, len(events))
	}
	if got := entries[0].Index.Projects; len(got) != 2 {
		t.Errorf("Projects = %v, want 2 entries", got)
	}
}

func TestIndexLoaderFiltersByProject(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC()
	writeFixture(t, dir, []model.LogEvent{
		makeEvent(base, "alpha", "x", "INFO", "a1"),
		makeEvent(base, "beta", "x", "INFO", "b1"),
	}, 1<<20)

	loader, _ := NewIndexLoader(dir, nil)
	got := loader.Filter(Query{Project: "alpha"})
	if len(got) != 1 {
		t.Fatalf("expected 1 entry for alpha, got %d", len(got))
	}
	if got := loader.Filter(Query{Project: "missing"}); len(got) != 0 {
		t.Errorf("expected 0 entries for missing project, got %d", len(got))
	}
}

func TestIndexLoaderFiltersByTime(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	writeFixture(t, dir, []model.LogEvent{
		makeEvent(base, "p", "x", "INFO", "jan"),
	}, 1<<20)

	loader, _ := NewIndexLoader(dir, nil)
	all := loader.Entries()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}

	q := Query{
		Project: "p",
		From:    base.Add(30 * 24 * time.Hour),
		To:      base.Add(31 * 24 * time.Hour),
	}
	if got := loader.Filter(q); len(got) != 0 {
		t.Errorf("expected 0 entries for time window after file, got %d", len(got))
	}

	q = Query{Project: "p", To: base.Add(time.Hour)}
	if got := loader.Filter(q); len(got) != 1 {
		t.Errorf("expected 1 entry for overlapping window, got %d", len(got))
	}
}

func TestQueryEndToEnd(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	events := []model.LogEvent{
		makeEvent(base, "billing", "req-1", "INFO", "first"),
		makeEvent(base.Add(time.Minute), "billing", "req-1", "ERROR", "second"),
		makeEvent(base.Add(2*time.Minute), "auth", "req-2", "INFO", "third"),
		makeEvent(base.Add(3*time.Minute), "billing", "req-9", "INFO", "fourth"),
	}
	writeFixture(t, dir, events, 1<<20)

	loader, _ := NewIndexLoader(dir, nil)
	engine := NewEngine(loader, nil)

	resp, err := engine.Execute(Query{Project: "billing"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Count != 3 {
		t.Errorf("count = %d, want 3", resp.Count)
	}
	for _, r := range resp.Results {
		if r.Project != "billing" {
			t.Errorf("unexpected project in result: %q", r.Project)
		}
	}

	resp, err = engine.Execute(Query{LogID: "req-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("count = %d, want 2", resp.Count)
	}

	if !resp.Results[0].Timestamp.After(resp.Results[1].Timestamp) {
		t.Errorf("default order is not desc: %v", resp.Results)
	}

	resp, err = engine.Execute(Query{Project: "billing", Limit: 1})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Count is the total matching rows; Results is the page.
	if resp.Count != 3 {
		t.Errorf("count = %d, want 3 (total matches)", resp.Count)
	}
	if len(resp.Results) != 1 {
		t.Errorf("limit not honoured: results = %d, want 1", len(resp.Results))
	}

	resp, err = engine.Execute(Query{Project: "billing", Order: OrderAsc, Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.Results[0].Timestamp.Before(resp.Results[len(resp.Results)-1].Timestamp) {
		t.Errorf("asc order wrong: %+v", resp.Results)
	}

	// Pagination: total count stays the same across pages, and rows
	// don't overlap or skip when stepping through offset. The fixture
	// has 3 billing events (req-1 INFO, req-1 ERROR, req-9 INFO).
	resp, err = engine.Execute(Query{Project: "billing", Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("Execute page 0: %v", err)
	}
	first := resp.Results
	if resp.Count != 3 || len(resp.Results) != 1 {
		t.Fatalf("page 0: count=%d results=%d, want 3/1", resp.Count, len(resp.Results))
	}

	resp, err = engine.Execute(Query{Project: "billing", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("Execute page 1: %v", err)
	}
	if resp.Count != 3 || len(resp.Results) != 2 {
		t.Fatalf("page 1: count=%d results=%d, want 3/2", resp.Count, len(resp.Results))
	}
	for _, r := range resp.Results {
		for _, prev := range first {
			if r.Timestamp == prev.Timestamp {
				t.Errorf("page 1 overlaps page 0: %v", r.Timestamp)
			}
		}
	}

	// Offset past the end returns an empty page with the same count.
	resp, err = engine.Execute(Query{Project: "billing", Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("Execute past end: %v", err)
	}
	if resp.Count != 3 || len(resp.Results) != 0 {
		t.Errorf("past-end: count=%d results=%d, want 3/0", resp.Count, len(resp.Results))
	}
}

func TestQueryNoMatches(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, []model.LogEvent{
		makeEvent(time.Now(), "p", "x", "INFO", "m"),
	}, 1<<20)
	loader, _ := NewIndexLoader(dir, nil)
	engine := NewEngine(loader, nil)
	resp, err := engine.Execute(Query{LogID: "no-such-id"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("expected 0, got %d", resp.Count)
	}
	if resp.Results == nil {
		t.Error("expected non-nil empty Results slice")
	}
}

// TestQuerySkipsCorruptParquet ensures a truncated parquet file in the
// index does not fail the whole query. This is the regression test for
// the "EOF reading magic header" error users saw when the collector was
// killed mid-rotation: the writer leaves a partial file with a sidecar,
// and the query engine must skip it instead of returning 500.
func TestQuerySkipsCorruptParquet(t *testing.T) {
	dir := t.TempDir()

	// One good file with a real project entry.
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	writeFixture(t, dir, []model.LogEvent{
		makeEvent(base, "billing", "req-1", "INFO", "ok"),
	}, 1<<20)

	// Plant a corrupt sidecar + zero-byte parquet file. Reload() will
	// reject it via parquet.Validate, so the loader never sees it.
	corruptBase := "logs-corrupt-test.parquet"
	if err := os.WriteFile(filepath.Join(dir, corruptBase+".idx.json"), []byte(`{"file":"logs-corrupt-test.parquet","started":"2026-09-06T12:00:00Z","ended":"2026-09-06T12:00:00Z","row_count":0,"projects":["billing"],"size_bytes":0}`), 0o644); err != nil {
		t.Fatalf("write corrupt sidecar: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, corruptBase), []byte("not a parquet file"), 0o644); err != nil {
		t.Fatalf("write corrupt parquet: %v", err)
	}

	loader, _ := NewIndexLoader(dir, nil)
	if n := loader.Count(); n != 1 {
		t.Fatalf("expected loader to skip the corrupt entry, got count=%d", n)
	}

	engine := NewEngine(loader, nil)
	resp, err := engine.Execute(Query{Project: "billing"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("expected 1 result from the good file, got %d", resp.Count)
	}
}

func TestHTTPHandlers(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	writeFixture(t, dir, []model.LogEvent{
		makeEvent(base, "billing", "req-1", "INFO", "ok"),
	}, 1<<20)
	loader, _ := NewIndexLoader(dir, nil)
	srv := NewServer(loader, nil)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/query")
	if err != nil {
		t.Fatalf("GET /query: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("missing-param status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/query?project=billing")
	if err != nil {
		t.Fatalf("GET /query?project=billing: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("query status = %d, want 200", resp.StatusCode)
	}
	defer resp.Body.Close()
	var got Response
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Count != 1 || got.Results[0].Project != "billing" {
		t.Errorf("unexpected response: %+v", got)
	}

	resp, err = http.Get(ts.URL + "/files")
	if err != nil {
		t.Fatalf("GET /files: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("files status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHTTPHealthzViaRun(t *testing.T) {
	dir := t.TempDir()
	loader, _ := NewIndexLoader(dir, nil)
	srv := NewServer(loader, nil)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestRunGracefulShutdown verifies that Run() unblocks when the context is
// cancelled and the listener is torn down.
func TestRunGracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	loader, _ := NewIndexLoader(dir, nil)
	srv := NewServer(loader, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx, addr, time.Second) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
