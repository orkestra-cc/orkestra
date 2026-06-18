// Command piiscan walks the Orkestra backend and flags modules that persist
// data-subject PII (a userUUID-style bson field) but register no
// iface.PIIProducer, so that personal data would escape the GDPR DSR
// export/erase sweep (ADR-0009). Exit code is non-zero when any
// error-severity diagnostic survives the baseline, which CI treats as a
// build failure.
//
// Usage:
//
//	go run ./tools/piiscan/cmd/piiscan \
//	    -baseline=tools/piiscan/baseline.txt \
//	    -markdown=pii-coverage-report.md \
//	    -json=pii-coverage-report.json \
//	    ./internal/...
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/orkestra/backend/tools/piiscan"
)

func main() {
	baseline := flag.String("baseline", "", "path to baseline file of accepted gaps (category:key per line)")
	markdown := flag.String("markdown", "", "write markdown report to this path (default: stdout)")
	jsonOut := flag.String("json", "", "write JSON report to this path")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./internal/..."}
	}

	baseSet, err := piiscan.LoadBaseline(*baseline)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	findings, err := piiscan.Scan(patterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	report := piiscan.Reconcile(findings, baseSet)

	if *markdown == "" {
		if err := piiscan.WriteMarkdown(os.Stdout, report, findings); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(2)
		}
	} else {
		f, err := os.Create(*markdown)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(2)
		}
		if err := piiscan.WriteMarkdown(f, report, findings); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(2)
		}
		f.Close()
	}

	if *jsonOut != "" {
		f, err := os.Create(*jsonOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(2)
		}
		if err := piiscan.WriteJSON(f, report); err != nil {
			f.Close()
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(2)
		}
		f.Close()
	}

	fmt.Fprintf(os.Stderr, "piiscan: %d errors, %d warnings, %d info\n",
		report.Summary[piiscan.SeverityError],
		report.Summary[piiscan.SeverityWarn],
		report.Summary[piiscan.SeverityInfo],
	)
	if report.HasErrors() {
		os.Exit(1)
	}
}
