// Package migreport writes the personal-data side output of a data migration
// (a per-row trace of what it changed): created exclusively (an existing path
// is an error, never overwritten), mode 0600, every cell neutralized because
// values come from third parties (names, titles, free text).
package migreport

import (
	"encoding/csv"
	"fmt"
	"os"

	"github.com/orkestra/backend/internal/shared/csvsafe"
)

type Writer struct {
	f *os.File
	w *csv.Writer
}

func Open(path string, header []string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("report %s: %w (refusing to overwrite)", path, err)
	}
	w := &Writer{f: f, w: csv.NewWriter(f)}
	if len(header) > 0 {
		if err := w.w.Write(header); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return w, nil
}

func (w *Writer) Write(row []string) error { return w.w.Write(csvsafe.Row(row)) }

func (w *Writer) Close() error {
	w.w.Flush()
	if err := w.w.Error(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}
