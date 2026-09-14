package parquet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcuspeh/logging-backend/internal/model"
)

// makeSealedFile writes a Parquet file + sidecar into a fresh sub-dir
// and returns the base name (no extension) plus the sub-dir. The
// caller is responsible for moving the file into the target directory
// if needed.
func makeSealedFile(t *testing.T, events []model.LogEvent) (string, string) {
	t.Helper()
	sub := t.TempDir()
	w, err := New(sub, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, ev := range events {
		if err := w.Write(ev); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(sub, "logs-*.parquet"))
	if len(files) != 1 {
		t.Fatalf("expected 1 sealed file, got %d (%v)", len(files), files)
	}
	return filepath.Base(files[0]), sub
}

// dropInto moves base (and base+idxSuffix) from srcDir into dstDir
// under a new base name. The new name is derived from the current unix
// nanosecond to guarantee uniqueness when multiple sealed files are
// produced within the same second.
func dropInto(t *testing.T, base, srcDir, dstDir string) string {
	t.Helper()
	unique := fmt.Sprintf("logs-%d-%04d.parquet", time.Now().UTC().UnixNano(), 0)
	for _, name := range []string{base, base + idxSuffix} {
		dst := unique
		if strings.HasSuffix(name, idxSuffix) {
			dst = unique + idxSuffix
		}
		if err := os.Rename(filepath.Join(srcDir, name), filepath.Join(dstDir, dst)); err != nil {
			t.Fatalf("rename %s -> %s: %v", name, dst, err)
		}
	}
	return strings.TrimSuffix(unique, ".parquet")
}

func TestCompactMergesSmallFiles(t *testing.T) {
	dir := t.TempDir()

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mk := func(offset int) []model.LogEvent {
		return []model.LogEvent{
			{Timestamp: base.Add(time.Duration(offset) * time.Second), Project: "p", LogID: "l", Level: "INFO", Message: "x"},
		}
	}

	for i := 1; i <= 3; i++ {
		baseName, sub := makeSealedFile(t, mk(i))
		dropInto(t, baseName, sub, dir)
	}

	compactor := NewCompactor(dir, 1<<20)

	w, err := New(dir, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("reopen New: %v", err)
	}
	defer w.Close()

	active := w.ActiveBaseName()
	if active == "" {
		t.Fatalf("expected non-empty active base name")
	}

	res, err := compactor.Compact(active, 1<<20)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Merged != 1 {
		t.Fatalf("Merged = %d, want 1; scanned=%d skipped=%d sources=%v",
			res.Merged, res.Scanned, res.Skipped, res.SourceFiles)
	}
	if res.RowsCompacted != 3 {
		t.Errorf("RowsCompacted = %d, want 3", res.RowsCompacted)
	}

	for _, base := range res.SourceFiles {
		if _, err := os.Stat(filepath.Join(dir, base)); !os.IsNotExist(err) {
			t.Errorf("expected source %s removed, err=%v", base, err)
		}
		if _, err := os.Stat(filepath.Join(dir, base+idxSuffix)); !os.IsNotExist(err) {
			t.Errorf("expected source idx %s removed, err=%v", base+idxSuffix, err)
		}
	}

	cFiles, _ := filepath.Glob(filepath.Join(dir, "compact-*.parquet"))
	if len(cFiles) != 1 {
		t.Fatalf("expected 1 compact-*.parquet, got %d (%v)", len(cFiles), cFiles)
	}
	idxBytes, err := os.ReadFile(cFiles[0] + idxSuffix)
	if err != nil {
		t.Fatalf("read compact idx: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(idxBytes, &idx); err != nil {
		t.Fatalf("unmarshal compact idx: %v", err)
	}
	if idx.RowCount != 3 {
		t.Errorf("merged index row_count = %d, want 3", idx.RowCount)
	}
	if len(idx.Projects) != 1 || idx.Projects[0] != "p" {
		t.Errorf("merged index projects = %v, want [p]", idx.Projects)
	}

	if _, err := os.Stat(filepath.Join(dir, active)); err != nil {
		t.Errorf("active file disappeared: %v", err)
	}
}

func TestCompactSkipsOversized(t *testing.T) {
	dir := t.TempDir()

	baseName, sub := makeSealedFile(t, []model.LogEvent{
		{Timestamp: time.Now().UTC(), Project: "p", LogID: "l", Level: "INFO", Message: "x"},
	})
	dropInto(t, baseName, sub, dir)

	compactor := NewCompactor(dir, 1)
	res, err := compactor.Compact("", 1<<20)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Merged != 0 {
		t.Errorf("Merged = %d, want 0 (all files oversized)", res.Merged)
	}
	if res.Skipped == 0 {
		t.Errorf("Skipped = 0, want > 0")
	}
}

func TestCompactSkipsActiveFile(t *testing.T) {
	dir := t.TempDir()

	w, err := New(dir, testOpts(1<<20, 0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	if err := w.Write(model.LogEvent{
		Timestamp: time.Now().UTC(), Project: "p", LogID: "l", Level: "INFO", Message: "x",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	compactor := NewCompactor(dir, 1<<20)
	active := w.ActiveBaseName()
	res, err := compactor.Compact(active, 1<<20)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Merged != 0 {
		t.Errorf("Merged = %d, want 0 (only the active file exists)", res.Merged)
	}
	if _, err := os.Stat(filepath.Join(dir, active)); err != nil {
		t.Errorf("active file vanished: %v", err)
	}
}

func TestCompactIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, ev := range []model.LogEvent{
		{Timestamp: base, Project: "p", LogID: "l", Level: "INFO", Message: "x"},
		{Timestamp: base.Add(time.Second), Project: "p", LogID: "l", Level: "INFO", Message: "x"},
	} {
		baseName, sub := makeSealedFile(t, []model.LogEvent{ev})
		dropInto(t, baseName, sub, dir)
	}

	compactor := NewCompactor(dir, 1<<20)

	res1, err := compactor.Compact("", 1<<20)
	if err != nil {
		t.Fatalf("Compact #1: %v", err)
	}
	if res1.Merged != 1 {
		t.Fatalf("Compact #1 Merged = %d, want 1", res1.Merged)
	}

	res2, err := compactor.Compact("", 1<<20)
	if err != nil {
		t.Fatalf("Compact #2: %v", err)
	}
	if res2.Merged != 0 {
		t.Errorf("Compact #2 Merged = %d, want 0 (idempotent)", res2.Merged)
	}
}
