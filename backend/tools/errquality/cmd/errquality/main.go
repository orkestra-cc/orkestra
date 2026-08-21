// Command errquality is the standalone runner for the errquality analyzer.
//
//	go run ./tools/errquality/cmd/errquality -baseline=tools/errquality/baseline.txt ./internal/...
//
// A non-zero exit code means one or more findings, which CI treats as a
// build failure.
package main

import (
	"github.com/orkestra/backend/tools/errquality"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(errquality.Analyzer)
}
