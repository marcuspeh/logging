package parquet

import (
	"errors"
	"io"
	"os"
)

// parquetMagic is the four-byte "PAR1" marker parquet-go writes at the
// start and end of every file. A file with the wrong magic is truncated
// or never finished flushing, and parquet-go will refuse to read it.
var parquetMagic = [4]byte{'P', 'A', 'R', '1'}

// ErrFileUnreadable is returned by Validate when a parquet file is too
// short to contain the parquet magic markers, or the markers are wrong.
// Callers should treat this as "skip this file" rather than fatal.
var ErrFileUnreadable = errors.New("parquet: file is unreadable (missing or corrupt magic)")

// Validate checks that path points to a fully-written Parquet file:
// the file must exist, be non-empty, and start AND end with the PAR1
// magic. A missing marker means the writer didn't finish — parquet-go
// returns "EOF" / "magic header" errors when asked to read such files.
//
// The check is intentionally cheap (two 4-byte reads + a stat) so the
// index loader can run it on every sidecar without measurable cost.
func Validate(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Size() < 8 {
		return ErrFileUnreadable
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return ErrFileUnreadable
	}
	if head != parquetMagic {
		return ErrFileUnreadable
	}

	// Footer magic sits in the last 4 bytes. parquet-go requires the
	// file length to be known when reading, but the writer may still be
	// appending; the sidecar's SizeBytes can lag by one flush. We trust
	// the on-disk size here.
	if _, err := f.Seek(-4, io.SeekEnd); err != nil {
		return ErrFileUnreadable
	}
	var tail [4]byte
	if _, err := io.ReadFull(f, tail[:]); err != nil {
		return ErrFileUnreadable
	}
	if tail != parquetMagic {
		return ErrFileUnreadable
	}
	return nil
}
