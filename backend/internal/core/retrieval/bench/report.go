package bench

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Mode names, in report order.
const (
	ModeKeyword      = "keyword"
	ModeDense        = "dense"
	ModeHybrid       = "hybrid"
	ModeHybridRerank = "hybrid+rerank"
)

// ModeResult is the outcome of one search mode over the fixture queries.
type ModeResult struct {
	Mode               string
	RecallAtK, NDCGAtK float64 // mean over queries with len(Expected) > 0
	P95                time.Duration
	Samples            int
	// VisibilityViolations counts returned ids that are not visible to the
	// query's principals.
	VisibilityViolations int
	Skipped              string // reason when the mode did not run
}

// Report is the result of one benchmark run.
type Report struct {
	Fixture                     string
	Items, Queries, K           int
	Dimensions                  int
	EmbedModel, EmbedRevision   string // as served, from GET /info
	RerankModel, RerankRevision string // as served; empty without a reranker
	IndexDuration               time.Duration
	ItemsPerSecond              float64
	PeakRSSBytes                int64 // VmHWM of /proc/self/status; 0 off Linux
	Modes                       []ModeResult
}

// OK reports whether no mode returned an item invisible to the query's
// principals.
func (r *Report) OK() bool {
	for _, m := range r.Modes {
		if m.VisibilityViolations > 0 {
			return false
		}
	}
	return true
}

// WriteText prints the report as plain text.
func (r *Report) WriteText(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "fixture    %s (%d items, %d queries, k=%d)\n", r.Fixture, r.Items, r.Queries, r.K)
	fmt.Fprintf(&b, "embed      %s (dimensions %d)\n", served(r.EmbedModel, r.EmbedRevision), r.Dimensions)
	fmt.Fprintf(&b, "rerank     %s\n", served(r.RerankModel, r.RerankRevision))
	fmt.Fprintf(&b, "indexing   %s, %.1f items/s", r.IndexDuration.Round(time.Millisecond), r.ItemsPerSecond)
	if r.PeakRSSBytes > 0 {
		fmt.Fprintf(&b, ", peak RSS %.1f MiB", float64(r.PeakRSSBytes)/(1<<20))
	}
	b.WriteString("\n\n")

	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "mode\trecall@%d\tndcg@%d\tp95\tsamples\tviolations\n", r.K, r.K)
	for _, m := range r.Modes {
		if m.Skipped != "" {
			fmt.Fprintf(tw, "%s\tskipped: %s\t\t\t\t\n", m.Mode, m.Skipped)
			continue
		}
		fmt.Fprintf(tw, "%s\t%.3f\t%.3f\t%s\t%d\t%s\n", m.Mode, m.RecallAtK, m.NDCGAtK,
			m.P95.Round(10*time.Microsecond), m.Samples, strconv.Itoa(m.VisibilityViolations))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	b.WriteString("\nviolations count returned ids invisible to the query principals. The engine driver\n" +
		"already drops out-of-scope candidates (and logs a warning), so a live run measures\n" +
		"driver and engine together: a non-zero value means both failed.\n")
	if r.OK() {
		b.WriteString("result: OK\n")
	} else {
		b.WriteString("result: FAIL (visibility violations)\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func served(model, revision string) string {
	if model == "" {
		return "none"
	}
	if revision == "" {
		return model
	}
	return model + "@" + revision
}

// peakRSS reads VmHWM (kB) from /proc/self/status; 0 when unavailable.
func peakRSS() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "VmHWM:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			return 0
		}
		kb, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			return 0
		}
		return kb << 10
	}
	return 0
}
