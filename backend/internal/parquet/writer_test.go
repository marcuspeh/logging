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

func TestNewCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "parquet")
	w, err := New(dir, 1024)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected dir created: %v", err)
	}
}

func TestNewRejectsBadArgs(t *testing.T) {
	if _, err := New("", 1); err == nil {
		t.Error("expected error for empty dir")
	}
	if _, err := New(t.TempDir(), 0); err == nil {
		t.Error("expected error for rotateBytes=0")
	}
}

func TestWriteAndCloseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 1<<20) // 1 MiB - no rotation expected
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
	w, err := New(dir, 200)
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
	w, err := New(dir, 1<<20)
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
