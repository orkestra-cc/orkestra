// Command logscope fails when an slog call the compliance masker cannot
// handle is not in the baseline (compliance spec §2.6).
//
//	go run ./tools/logscope/cmd/logscope -baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
//	go run ./tools/logscope/cmd/logscope -write-baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/orkestra/backend/tools/logscope"
	"github.com/orkestra/backend/tools/piiscan"
)

func main() {
	baseline := flag.String("baseline", "", "baseline file (category:file:func:key per line)")
	write := flag.String("write-baseline", "", "write every current finding to this file and exit 0")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./internal/...", "./pkg/...", "./cmd/..."}
	}
	findings, err := logscope.Scan(".", patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *write != "" {
		keys := map[string]bool{}
		for _, f := range findings {
			keys[f.BaselineKey()] = true
		}
		lines := make([]string, 0, len(keys))
		for k := range keys {
			lines = append(lines, k)
		}
		sort.Strings(lines)
		header := "# logscope baseline — existing slog calls the compliance masker cannot mask reliably.\n# Remove a line when the call is fixed; never add one by hand for new code.\n"
		if err := os.WriteFile(*write, []byte(header+strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	base, err := piiscan.LoadBaseline(*baseline)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	failed := 0
	for _, f := range findings {
		if base[f.BaselineKey()] {
			continue
		}
		failed++
		fmt.Printf("%s:%d: %s key %q in %s\n", f.File, f.Line, f.Category, f.Key, f.Func)
	}
	if failed > 0 {
		fmt.Printf("\nlogscope: %d new finding(s). Log a map, a slice, basic values or an error instead of an opaque value, and keep secrets out of logs.\n", failed)
		os.Exit(1)
	}
}
