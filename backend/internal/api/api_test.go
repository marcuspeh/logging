package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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

	loader, err := NewIndexLoader(dir)
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

	loader, _ := NewIndexLoader(dir)
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

	loader, _ := NewIndexLoader(dir)
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

	loader, _ := NewIndexLoader(dir)
	engine := NewEngine(loader)

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
	if resp.Count != 1 {
		t.Errorf("limit not honoured: count = %d", resp.Count)
	}

	resp, err = engine.Execute(Query{Project: "billing", Order: OrderAsc, Limit: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !resp.Results[0].Timestamp.Before(resp.Results[len(resp.Results)-1].Timestamp) {
		t.Errorf("asc order wrong: %+v", resp.Results)
	}
}

func TestQueryNoMatches(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, []model.LogEvent{
		makeEvent(time.Now(), "p", "x", "INFO", "m"),
	}, 1<<20)
	loader, _ := NewIndexLoader(dir)
	engine := NewEngine(loader)
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

func TestHTTPHandlers(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	writeFixture(t, dir, []model.LogEvent{
		makeEvent(base, "billing", "req-1", "INFO", "ok"),
	}, 1<<20)
	loader, _ := NewIndexLoader(dir)
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
	loader, _ := NewIndexLoader(dir)
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
	loader, _ := NewIndexLoader(dir)
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
