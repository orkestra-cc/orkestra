package piiscan

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadBaseline reads a baseline file and returns the set of entries the
// reporter should suppress. Entries are one per line in the form
// "category:key" (e.g. "pii.subject_collection.no_producer:compliance");
// blank lines and lines starting with "#" are ignored.
//
// The baseline exists so modules whose subject data is retained by design
// (an audit trail, legal-held rows) are not erroneously required to expose a
// PIIProducer. New gaps are always flagged because they are not in the file.
func LoadBaseline(path string) (map[string]bool, error) {
	if path == "" {
		return map[string]bool{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("piiscan: open baseline %s: %w", path, err)
	}
	defer f.Close()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("piiscan: read baseline: %w", err)
	}
	return out, nil
}
