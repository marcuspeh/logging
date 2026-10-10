package parquet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/marcuspeh/logging-backend/internal/model"
)

// testOpts returns FlushOptions suitable for unit tests: small flush
// thresholds so the size-rotate and time-rotate paths can be exercised
// without long sleeps.
func testOpts(rotateBytes int64, rotateEvery time.Duration) FlushOptions {
	return FlushOptions{
		RotateBytes: rotateBytes,
		RotateEvery: rotateEvery,
		FlushRows:   1024,
		FlushEvery:  5 * time.Second,
	}
}

func TestNewCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "parquet")
	w, err := New(dir, testOpts(1024, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected dir created: %v", err)
	}
}

func TestNewRejectsBadArgs(t *testing.T) {
	if _, err := New("", testOpts(1, 0)); err == nil {
		t.Error("expected error for empty dir")
	}
	if _, err := New(t.TempDir(), testOpts(0, 0)); err == nil {
		t.Error("expected error for rotateBytes=0")
	}
}

// TestCloseOnEmptyRemovesStub is a regression test for an incident where
// the writer opened a parquet file at startup, received no events, then
// had its Close() called (e.g. shutdown, or a fast restart after
// startup). Close() used to write a sidecar with row_count=0 and zero
// timestamps — the index loader accepted it as a valid file, the query
// engine returned no rows for the file's time range, and effectively
// shadowed other files with the same time range.
func TestCloseOnEmptyRemovesStub(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, testOpts(1024, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	active := filepath.Base(w.ActiveBaseName())

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Empty parquet stub should be gone.
	pqPath := filepath.Join(dir, active)
	if _, err := os.Stat(pqPath); !os.IsNotExist(err) {
		t.Errorf("expected empty parquet stub removed, got err=%v", err)
	}
	// Sidecar should not have been written.
	idxPath := pqPath + idxSuffix
	if _, err := os.Stat(idxPath); !os.IsNotExist(err) {
		t.Errorf("expected no sidecar for empty parquet, got err=%v", err)
	}
}

// TestCloseAfterWritesStillEmitsSidecar guards the empty-removal path:
// once we DO write rows, Close() must still produce a correct sidecar.
func TestCloseAfterWritesStillEmitsSidecar(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, testOpts(1024, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ts := time.Date(2026, 9, 28, 16, 15, 0, 0, time.UTC)
	if err := w.Write(model.LogEvent{
		Timestamp: ts,
		Project:   "algo01-corner2rsi",
		LogID:     "-",
		Level:     "INFO",
		Message:   "hello",
		Caller:    "main.go:52",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Both parquet and sidecar must exist.
	matches, err := filepath.Glob(filepath.Join(dir, "*"+idxSuffix))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 sidecar, got %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parse sidecar: %v", err)
	}
	if idx.RowCount != 1 {
		t.Errorf("row_count = %d, want 1", idx.RowCount)
	}
	if len(idx.Projects) != 1 || idx.Projects[0] != "algo01-corner2rsi" {
		t.Errorf("projects = %v, want [algo01-corner2rsi]", idx.Projects)
	}
	if !idx.Started.Equal(ts) || !idx.Ended.Equal(ts) {
		t.Errorf("started/ended = %v / %v, want %v", idx.Started, idx.Ended, ts)
	}
}

// TestFlushAcknowledgesRowsWithoutRotating pins the new durability
// contract: Flush is now a cheap acknowledgement and must not seal a
// file, since sealing on every flush drove file counts proportional to
// wall-clock time. Rows remain visible via the tail buffer; they hit
// disk on Rotate (or via Close). The previous version of this test
// asserted the opposite behaviour.
func TestFlushAcknowledgesRowsWithoutRotating(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, testOpts(1024, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	ts := time.Date(2026, 9, 29, 11, 50, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := w.Write(model.LogEvent{
			Timestamp: ts.Add(time.Duration(i) * time.Second),
			Project:   "algo01-corner2rsi",
			LogID:     "flush-test",
			Level:     "INFO",
			Message:   "pending",
			Caller:    "writer_test.go:flush",
		}); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// No sidecar should have been written by Flush — the file is
	// still being written to and is not yet sealed.
	matches, err := filepath.Glob(filepath.Join(dir, "*"+idxSuffix))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no sidecars after Flush, got %d", len(matches))
	}

	// Rows are still queryable from the tail buffer.
	tail := w.Tail()
	if len(tail) != 3 {
		t.Fatalf("tail length = %d, want 3", len(tail))
	}

	// An explicit Rotate seals the file. This is the path that gives
	// rows their durable sidecar.
	if err := w.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	matches, err = filepath.Glob(filepath.Join(dir, "*"+idxSuffix))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 sealed sidecar after Rotate, got %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parse sidecar: %v", err)
	}
	if idx.RowCount != 3 {
		t.Errorf("sealed row_count = %d, want 3", idx.RowCount)
	}
	if idx.SizeBytes == 0 {
		t.Errorf("sealed size_bytes = 0, want > 0 (rows must be on disk)")
	}

	// After Rotate, the tail should be drained so events aren't served
	// from both the tail and the now-sealed file.
	if tail := w.Tail(); len(tail) != 0 {
		t.Errorf("tail length after Rotate = %d, want 0", len(tail))
	}
}

func TestWriteAndCloseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, testOpts(1<<20, 0)) // 1 MiB - no rotation expected
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	events := []model.LogEvent{
		{Timestamp: base, Project: "billing", LogID: "req-1", Level: "INFO", Message: "ok"},
		{Timestamp: base.Add(time.Second), Project: "billing", LogID: "req-1", Level: "ERROR", Message: "failed"},
		{Timestamp: base.Add(2 * time.Second), Project: "auth", LogID: "req-2", Level: "INFO", Message: "login"},
	}
	for _, e := range events {
		if err := w.Write(e); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "logs-*.parquet"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 parquet file, got %d (%v)", len(files), files)
	}
	pq := files[0]

	idxPath := pq + idxSuffix
	idxBytes, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(idxBytes, &idx); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if idx.RowCount != int64(len(events)) {
		t.Errorf("index row_count = %d, want %d", idx.RowCount, len(events))
	}
	wantProjects := []string{"auth", "billing"}
	if !equalStrings(idx.Projects, wantProjects) {
		t.Errorf("index projects = %v, want %v", idx.Projects, wantProjects)
	}
	if idx.SizeBytes <= 0 {
		t.Errorf("index size_bytes = %d, want > 0", idx.SizeBytes)
	}

	pf, err := os.Open(pq)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer pf.Close()

	st, err := pf.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	rows, err := parquet.Read[model.LogEvent](pf, st.Size())
	if err != nil {
		t.Fatalf("read parquet: %v", err)
	}
	if len(rows) != len(events) {
		t.Fatalf("round-trip row count = %d, want %d", len(rows), len(events))
	}
	gotProjects := make(map[string]int)
	for _, r := range rows {
		gotProjects[r.Project]++
	}
	if gotProjects["billing"] != 2 || gotProjects["auth"] != 1 {
		t.Errorf("round-trip project counts = %v", gotProjects)
	}
}

func TestRotationProducesMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	// Tiny rotation threshold forces rotation on a small payload.
	w, err := New(dir, testOpts(200, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	const N = 200
	for i := 0; i < N; i++ {
		ev := model.LogEvent{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Project:   "rot",
			LogID:     "id",
			Level:     "INFO",
			Message:   "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", // 50 bytes
		}
		if err := w.Write(ev); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "logs-*.parquet"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected >=2 parquet files due to rotation, got %d (%v)", len(files), files)
	}

	for _, f := range files {
		idxPath := f + idxSuffix
		if _, err := os.Stat(idxPath); err != nil {
			t.Errorf("missing index for %s: %v", filepath.Base(f), err)
		}
	}
}

func TestExplicitRotate(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < 5; i++ {
		_ = w.Write(model.LogEvent{
			Timestamp: time.Now().UTC(),
			Project:   "p", LogID: "l", Level: "INFO", Message: "m",
		})
	}
	if err := w.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "logs-*.parquet"))
	if len(files) != 2 {
		t.Errorf("after explicit Rotate, expected 2 files, got %d", len(files))
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close after explicit Rotate: %v", err)
	}
}

func TestSweepRetention(t *testing.T) {
	dir := t.TempDir()

	// Two sealed files (old + fresh) and one active file (recent).
	w, err := New(dir, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	write := func(ts time.Time) {
		_ = w.Write(model.LogEvent{
			Timestamp: ts, Project: "p", LogID: "l", Level: "INFO", Message: "m",
		})
	}

	old := time.Now().Add(-30 * 24 * time.Hour) // older than TTL
	mid := time.Now().Add(-10 * 24 * time.Hour) // within TTL
	fresh := time.Now().Add(-1 * time.Hour)     // within TTL

	write(old)
	if err := w.Rotate(); err != nil {
		t.Fatalf("Rotate after old: %v", err)
	}
	write(mid)
	if err := w.Rotate(); err != nil {
		t.Fatalf("Rotate after mid: %v", err)
	}
	write(fresh) // stays in the active file
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen writer for retention sweep — the active file from Close()
	// is now a sealed file too, but a fresh writer has no active file.
	w2, err := New(dir, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("reopen New: %v", err)
	}
	defer w2.Close()

	res, err := w2.SweepRetention(time.Now(), 14*24*time.Hour)
	if err != nil {
		t.Fatalf("SweepRetention: %v", err)
	}
	if len(res.Deleted) != 1 {
		t.Errorf("Deleted = %d, want 1 (the old file); deleted=%v scanned=%d skipped=%d",
			len(res.Deleted), res.Deleted, res.Scanned, res.Skipped)
	}
	if res.BytesFreed <= 0 {
		t.Errorf("BytesFreed = %d, want > 0", res.BytesFreed)
	}

	// Confirm the old parquet + its sidecar are gone.
	for _, base := range res.Deleted {
		if _, err := os.Stat(filepath.Join(dir, base)); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, err=%v", base, err)
		}
		if _, err := os.Stat(filepath.Join(dir, base+idxSuffix)); !os.IsNotExist(err) {
			t.Errorf("expected %s index removed, err=%v", base, err)
		}
	}
}

func TestSweepRetentionNeverDeletesActive(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Backdate the only file via a synthetic old index, then verify
	// SweepRetention leaves it alone because it's the active file.
	_ = w.Write(model.LogEvent{
		Timestamp: time.Now().Add(-30 * 24 * time.Hour),
		Project:   "p", LogID: "l", Level: "INFO", Message: "m",
	})

	// Active file is still open (no Rotate/Close). Force its sidecar
	// index to look old by manually rewriting timestamps.
	files, _ := filepath.Glob(filepath.Join(dir, "logs-*.parquet"))
	if len(files) != 1 {
		t.Fatalf("expected 1 active file, got %d", len(files))
	}
	idxPath := files[0] + idxSuffix
	idx := Index{
		File:      filepath.Base(files[0]),
		Started:   time.Now().Add(-30 * 24 * time.Hour),
		Ended:     time.Now().Add(-30 * 24 * time.Hour),
		RowCount:  1,
		Projects:  []string{"p"},
		SizeBytes: 0,
	}
	b, _ := json.Marshal(idx)
	if err := os.WriteFile(idxPath, b, 0o644); err != nil {
		t.Fatalf("rewrite index: %v", err)
	}

	res, err := w.SweepRetention(time.Now(), 14*24*time.Hour)
	if err != nil {
		t.Fatalf("SweepRetention: %v", err)
	}
	if len(res.Deleted) != 0 {
		t.Errorf("active file must not be deleted; deleted=%v", res.Deleted)
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Errorf("active file disappeared: %v", err)
	}
}

// TestOldSchemaParquetRoundTrip writes a Parquet file with the
// pre-caller schema (no "caller" column) and asserts the new code can
// read it back with Caller == "". This is the on-disk half of the
// backward-compatibility contract: a backend running the new code
// must not error when a Parquet file from an older backend lands on
// disk via restore/upgrade.
func TestOldSchemaParquetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-schema.parquet")

	// Use a local struct that mirrors model.LogEvent but WITHOUT
	// the Caller column. parquet-go will materialise a file with
	// a 5-column schema.
	type oldEvent struct {
		Timestamp time.Time `parquet:"timestamp"`
		Project   string    `parquet:"project"`
		LogID     string    `parquet:"logid"`
		Level     string    `parquet:"level"`
		Message   string    `parquet:"message"`
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pw := parquet.NewGenericWriter[oldEvent](f)
	ts := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := pw.Write([]oldEvent{
		{Timestamp: ts, Project: "billing", LogID: "r1", Level: "INFO", Message: "hi"},
	}); err != nil {
		t.Fatalf("write old-schema: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	// Read it back with the new model.LogEvent (which has Caller).
	rf, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rf.Close()
	st, err := rf.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	rows, err := parquet.Read[model.LogEvent](rf, st.Size())
	if err != nil {
		// Some versions of parquet-go reject missing columns
		// outright. Surface that as a clear failure rather than
		// letting it masquerade as a different bug.
		t.Fatalf("read new-schema over old-schema file: %v (parquet-go missing-column path)", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Caller != "" {
		t.Errorf("Caller = %q, want \"\" for old-schema row", rows[0].Caller)
	}
	if rows[0].Message != "hi" || rows[0].Project != "billing" {
		t.Errorf("row payload mismatch: %+v", rows[0])
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
