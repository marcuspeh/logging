package parquet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/marcuspeh/logging-backend/internal/model"
)

// compactPrefix marks sealed files produced by the compactor. Files
// already carrying this prefix are skipped so compaction is idempotent.
const compactPrefix = "compact-"

// compactCandidate is a sealed file eligible for compaction.
type compactCandidate struct {
	base string
	path string
	idx  Index
	size int64
}

// Compactor merges small sealed Parquet files in a directory into a
// single compacted file, then deletes the originals.
//
// Compaction rules (matches retention's safety stance):
//   - The writer's currently-active file is never a source.
//   - Files larger than maxFileBytes are skipped (already "big enough").
//   - The output filename carries `compact-` so a second pass won't
//     re-merge the merged file.
//   - Index files must be parseable; corrupt/missing sidecars skip.
//   - Originals are removed only after the merged file is renamed into
//     place, so a crash mid-run leaves the directory in a valid state
//     (sum of rows unchanged or larger, never smaller).
type Compactor struct {
	dir          string
	maxFileBytes int64
}

// NewCompactor returns a compactor that scans dir and merges sealed
// files whose sidecar reports SizeBytes <= maxFileBytes.
func NewCompactor(dir string, maxFileBytes int64) *Compactor {
	return &Compactor{dir: dir, maxFileBytes: maxFileBytes}
}

// CompactResult summarises one compaction run.
type CompactResult struct {
	Scanned       int      // sealed index files inspected
	Merged        int      // compacted files written
	SourceFiles   []string // base names of files removed after a successful merge
	BytesFreed    int64    // bytes freed by removing source files (before merge output)
	RowsCompacted int64    // rows copied into merged files
	Skipped       int      // files skipped (active, oversized, corrupt index, already compact)
}

// Compact scans dir for sealed files smaller than maxFileBytes and
// groups them into batches by approximate total size (rotateBytes), then
// writes one merged file per batch. Returns immediately if no
// candidates are found. `activeFn` returns the base name of the writer's
// current Parquet file (or "" if none) — it's called before every batch
// flush so a writer rotation mid-compaction is observed. The active
// name is never used as a compaction source.
func (c *Compactor) Compact(activeFn func() string, rotateBytes int64) (CompactResult, error) {
	var res CompactResult

	if rotateBytes <= 0 {
		return res, fmt.Errorf("parquet: compact: rotateBytes must be > 0, got %d", rotateBytes)
	}

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return res, fmt.Errorf("parquet: compact: read dir: %w", err)
	}

	var batch []compactCandidate
	batchBytes := int64(0)
	leftover := []string{}

	// freshenActive asks the writer (under its own mutex) which file is
	// currently being written. The active name is never a compaction
	// source. Re-evaluated before every batch flush so a writer rotation
	// mid-compaction doesn't leave the new active file eligible.
	freshenActive := func() string {
		if activeFn == nil {
			return ""
		}
		return activeFn()
	}

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		// Re-check the active file right before we touch the merged
		// output — the writer may have rotated while we were scanning.
		active := freshenActive()

		// Drop any source whose name now equals the active file
		// (writer rotated and now we're about to delete a live file).
		filtered := batch[:0]
		for _, src := range batch {
			if src.base == active {
				leftover = append(leftover, src.base)
				continue
			}
			filtered = append(filtered, src)
		}
		batch = filtered
		if len(batch) < 2 {
			// Single (or zero) non-active candidates — nothing to merge.
			batch = batch[:0]
			batchBytes = 0
			return nil
		}

		merged, rows, err := c.mergeBatch(batch)
		if err != nil {
			return err
		}
		if !merged {
			batch = batch[:0]
			batchBytes = 0
			return nil
		}
		for _, src := range batch {
			if err := os.Remove(src.path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("parquet: compact: remove %s: %w", src.path, err)
			}
			if err := os.Remove(src.path + idxSuffix); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("parquet: compact: remove %s: %w", src.path+idxSuffix, err)
			}
			res.BytesFreed += src.size
			res.SourceFiles = append(res.SourceFiles, src.base)
		}
		res.RowsCompacted += rows
		res.Merged++
		batch = batch[:0]
		batchBytes = 0
		return nil
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, idxSuffix) {
			continue
		}
		if strings.HasPrefix(name, compactPrefix) {
			continue
		}
		res.Scanned++

		base := strings.TrimSuffix(name, idxSuffix)
		if base == freshenActive() {
			res.Skipped++
			continue
		}
		if strings.HasPrefix(base, compactPrefix) {
			res.Skipped++
			continue
		}
		// Re-check the active file once more right before we commit
		// to merging it. The writer may have rotated between the
		// initial filter above and now.
		if base == freshenActive() {
			res.Skipped++
			continue
		}

		idxPath := filepath.Join(c.dir, name)
		data, err := os.ReadFile(idxPath)
		if err != nil {
			res.Skipped++
			continue
		}
		var idx Index
		if err := json.Unmarshal(data, &idx); err != nil {
			res.Skipped++
			continue
		}
		pqPath := filepath.Join(c.dir, base)
		st, err := os.Stat(pqPath)
		if err != nil {
			res.Skipped++
			continue
		}
		if c.maxFileBytes > 0 && st.Size() > c.maxFileBytes {
			res.Skipped++
			continue
		}

		batch = append(batch, compactCandidate{base: base, path: pqPath, idx: idx, size: st.Size()})
		batchBytes += st.Size()

		if batchBytes >= rotateBytes {
			if err := flush(); err != nil {
				return res, err
			}
		}
	}
	if err := flush(); err != nil {
		return res, err
	}
	if len(leftover) > 0 {
		res.Skipped += len(leftover)
	}
	return res, nil
}

// mergeBatch reads rows from every source file, writes them into a new
// compacted Parquet file with its sidecar index, and returns whether a
// merged file was produced plus the row count. Returns (false, 0, nil)
// when a single-file batch wouldn't benefit from rewriting (size
// already equals what we'd produce).
func (c *Compactor) mergeBatch(batch []compactCandidate) (bool, int64, error) {
	if len(batch) == 0 {
		return false, 0, nil
	}
	if len(batch) == 1 {
		return false, 0, nil
	}

	rows := make([]model.LogEvent, 0, 1024)
	minTs := time.Time{}
	maxTs := time.Time{}
	projects := make(map[string]struct{})

	for _, src := range batch {
		f, err := os.Open(src.path)
		if err != nil {
			return false, 0, fmt.Errorf("parquet: compact: open %s: %w", src.path, err)
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return false, 0, fmt.Errorf("parquet: compact: stat %s: %w", src.path, err)
		}
		rs, err := parquet.Read[model.LogEvent](f, st.Size())
		if err != nil {
			f.Close()
			return false, 0, fmt.Errorf("parquet: compact: read %s: %w", src.path, err)
		}
		f.Close()

		for i := range rs {
			ev := rs[i]
			if minTs.IsZero() || ev.Timestamp.Before(minTs) {
				minTs = ev.Timestamp
			}
			if ev.Timestamp.After(maxTs) {
				maxTs = ev.Timestamp
			}
			if ev.Project != "" {
				projects[ev.Project] = struct{}{}
			}
			rows = append(rows, ev)
		}
	}

	if len(rows) == 0 {
		return false, 0, nil
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Timestamp.Before(rows[j].Timestamp)
	})

	now := time.Now().UTC()
	seq := nextCompactSeq(c.dir, now.Unix())
	outName := fmt.Sprintf("%s%d-%04d.parquet", compactPrefix, now.Unix(), seq)
	outPath := filepath.Join(c.dir, outName)
	tmpPath := outPath + ".tmp"

	out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_TRUNC, 0o644)
	if err != nil {
		return false, 0, fmt.Errorf("parquet: compact: create %s: %w", tmpPath, err)
	}

	pw := parquet.NewGenericWriter[model.LogEvent](out,
		parquet.Compression(&parquet.Snappy),
		parquet.MaxRowsPerRowGroup(64*1024),
	)
	if _, err := pw.Write(rows); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("parquet: compact: write rows: %w", err)
	}
	if err := pw.Close(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("parquet: compact: close writer: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("parquet: compact: fsync: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("parquet: compact: close file: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		os.Remove(tmpPath)
		return false, 0, fmt.Errorf("parquet: compact: rename: %w", err)
	}

	st, err := os.Stat(outPath)
	if err != nil {
		return false, 0, fmt.Errorf("parquet: compact: stat result: %w", err)
	}
	idx := Index{
		File:      outName,
		Started:   minTs,
		Ended:     maxTs,
		RowCount:  int64(len(rows)),
		Projects:  sortedKeys(projects),
		SizeBytes: st.Size(),
	}
	if err := writeIndex(outPath, idx); err != nil {
		return false, 0, err
	}
	return true, int64(len(rows)), nil
}

// nextCompactSeq scans dir for the highest sequence suffix used by
// files matching `<compactPrefix><unix>-NNNN.parquet` and returns the
// next free sequence. Returns 0 when the directory contains no
// compacted files for this second.
func nextCompactSeq(dir string, unix int64) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	prefix := fmt.Sprintf("%s%d-", compactPrefix, unix)
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
