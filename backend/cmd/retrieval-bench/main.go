// Command retrieval-bench runs the ADR-0023 D14 benchmark against the
// retrieval overlay: it indexes a fixture into a scratch Qdrant collection and
// measures keyword, dense, hybrid and hybrid+rerank search.
//
// It imports the llm module's internal installation package on purpose
// (documented exception, ADR-0023 PR 8): the benchmark must read
// RETRIEVAL_INFERENCE_ENDPOINTS with the parser the backend uses, so it
// measures exactly the models, prefixes and flags the backend would load.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/orkestra/backend/internal/core/llm/installation"
	"github.com/orkestra/backend/internal/core/retrieval/bench"
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/qdrant"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	sharedKeyEnv = "RETRIEVAL_INFERENCE_API_KEY"
	httpTimeout  = 2 * time.Minute // per TEI call; Run's context is the outer bound
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }

// engineFactory is the seam tests replace; newEngine is the only call into
// the Qdrant driver.
var engineFactory = newEngine

// newEngine builds the Qdrant engine. The driver never dials here.
func newEngine(url, apiKey string) (engine.Engine, error) {
	d, err := qdrant.New(qdrant.Config{URL: url, APIKey: apiKey})
	if err != nil {
		return nil, err // untyped nil engine, not a nil *Driver
	}
	return d, nil
}

// resolveEndpoint returns the named endpoint of RETRIEVAL_INFERENCE_ENDPOINTS
// as the backend's own parser reads it, so the key is getenv(apiKeyEnv) when
// the entry names one, else RETRIEVAL_INFERENCE_API_KEY. productionLike is
// true for ENV staging or production.
func resolveEndpoint(endpointsJSON, name string, getenv func(string) string, productionLike bool) (installation.Endpoint, error) {
	eps, warns := installation.Parse(endpointsJSON, getenv, productionLike)
	for _, ep := range eps {
		if ep.Name == name {
			return ep, nil
		}
	}
	msg := fmt.Sprintf("endpoint %q not found in %s", name, installation.EndpointsEnv)
	if len(warns) > 0 {
		msg += " (" + strings.Join(warns, "; ") + ")"
	}
	return installation.Endpoint{}, errors.New(msg)
}

type options struct {
	fixture, tenant, embedEndpoint, rerankEndpoint, embedURL, rerankURL, jsonPath string
	k, perArm, rerankTop, repeat, batch                                           int
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("retrieval-bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.fixture, "fixture", "", "benchmark fixture (JSONL), required")
	fs.IntVar(&o.k, "k", 10, "results per query")
	fs.IntVar(&o.perArm, "per-arm", 50, "candidates per search arm")
	fs.IntVar(&o.rerankTop, "rerank-top", 20, "candidates sent to the reranker")
	fs.IntVar(&o.repeat, "repeat", 3, "latency samples per query and mode")
	fs.IntVar(&o.batch, "batch", 32, "embedding batch size")
	fs.StringVar(&o.tenant, "tenant", "t-bench", "tenant id of the scratch data")
	fs.StringVar(&o.embedEndpoint, "embed-endpoint", "embed", "embed endpoint name in "+installation.EndpointsEnv)
	fs.StringVar(&o.rerankEndpoint, "rerank-endpoint", "rerank", "rerank endpoint name (empty: skip rerank)")
	fs.StringVar(&o.embedURL, "embed-url", "", "embed TEI URL, overrides the endpoint list")
	fs.StringVar(&o.rerankURL, "rerank-url", "", "rerank TEI URL, overrides the endpoint list")
	fs.StringVar(&o.jsonPath, "json", "", "also write the report as JSON to this path")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	// Parsing stops at the first non-flag, so a stray argument would silently
	// drop every flag after it (and benchmark the default fixture).
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q (flags only; use -fixture)", fs.Arg(0))
	}
	if o.fixture == "" {
		return o, errors.New("-fixture is required")
	}
	return o, nil
}

// run parses flags and env, runs the benchmark and prints the report:
// 0 ok, 1 run error or visibility violations, 2 usage (production included).
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	o, err := parseFlags(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "retrieval-bench:", err)
		}
		return 2
	}
	// orkestra.sh refuses production too; this holds for a binary built and
	// run elsewhere: the bench loads the inference servers for minutes and
	// writes a scratch collection into the serving Qdrant.
	if strings.TrimSpace(getenv("ENV")) == "production" {
		fmt.Fprintln(stderr, "retrieval-bench: refusing to run with ENV=production; benchmark a development or staging stack")
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "retrieval-bench:", err)
		return 1
	}

	qdrantURL := strings.TrimSpace(getenv("QDRANT_URL"))
	if qdrantURL == "" {
		return fail(errors.New("QDRANT_URL is not set — start the overlay (./orkestra.sh retrieval up)"))
	}
	// Run builds its scratch collection with the prefix "bench"; a module
	// corpus under the same prefix could collide with it.
	if strings.TrimSpace(getenv("QDRANT_COLLECTION_PREFIX")) == "bench" {
		return fail(errors.New("QDRANT_COLLECTION_PREFIX=bench would collide with the benchmark's scratch collections; use another prefix"))
	}

	productionLike := getenv("ENV") == "staging" || getenv("ENV") == "production"
	embedEP, embedDeclared, err := pickEndpoint(o.embedURL, o.embedEndpoint, iface.InferenceKindEmbed, getenv, productionLike)
	if err != nil {
		return fail(err)
	}
	var rerankEP installation.Endpoint
	var rerankDeclared bool
	if o.rerankURL != "" || o.rerankEndpoint != "" {
		if rerankEP, rerankDeclared, err = pickEndpoint(o.rerankURL, o.rerankEndpoint, iface.InferenceKindRerank, getenv, productionLike); err != nil {
			return fail(err)
		}
	}

	f, err := os.Open(o.fixture)
	if err != nil {
		return fail(err)
	}
	fx, err := bench.LoadFixture(f)
	_ = f.Close()
	if err != nil {
		return fail(fmt.Errorf("fixture %s: %w", o.fixture, err))
	}

	eng, err := engineFactory(qdrantURL, getenv("QDRANT_API_KEY"))
	if err != nil {
		return fail(fmt.Errorf("qdrant: %w", err))
	}

	hc := &http.Client{Timeout: httpTimeout}
	cfg := bench.Config{
		Engine:         eng,
		Embed:          &bench.TEIClient{BaseURL: embedEP.URL, APIKey: embedEP.APIKey, Normalize: embedEP.Normalize, Truncate: embedEP.Truncate, HTTP: hc},
		QueryPrefix:    embedEP.QueryPrefix,
		DocumentPrefix: embedEP.DocumentPrefix,
		TenantID:       o.tenant,
		K:              o.k,
		PerArm:         o.perArm,
		RerankTopK:     o.rerankTop,
		Repeat:         o.repeat,
		BatchSize:      o.batch,
	}
	// The declared identity is checked only for entries from the endpoint
	// list; a URL override benchmarks whatever the server serves and the
	// report prints it.
	if embedDeclared {
		cfg.Declared.EmbedModel, cfg.Declared.EmbedRevision = embedEP.Model, embedEP.Revision
	}
	if rerankEP.URL != "" {
		cfg.Rerank = &bench.TEIClient{BaseURL: rerankEP.URL, APIKey: rerankEP.APIKey, Truncate: rerankEP.Truncate, HTTP: hc}
		if rerankDeclared {
			cfg.Declared.RerankModel, cfg.Declared.RerankRevision = rerankEP.Model, rerankEP.Revision
		}
	}

	// SIGTERM and SIGHUP (a closed terminal, a stopped exec) cancel the run
	// like Ctrl-C, so the scratch collection is still dropped.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	rep, runErr := bench.Run(ctx, cfg, fx, o.fixture)
	if rep == nil {
		return fail(runErr)
	}
	if err := rep.WriteText(stdout); err != nil {
		return fail(err)
	}
	if o.jsonPath != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(o.jsonPath, append(b, '\n'), 0o600); err != nil {
			return fail(err)
		}
	}
	if runErr != nil {
		return fail(runErr)
	}
	if !rep.OK() {
		return 1
	}
	return 0
}

// pickEndpoint returns the endpoint to benchmark and whether it comes from the
// declared list. A URL override is synthesized with the shared key and the
// production request flags and declares no model.
func pickEndpoint(url, name string, kind iface.InferenceKind, getenv func(string) string, productionLike bool) (installation.Endpoint, bool, error) {
	if url != "" {
		return installation.Endpoint{URL: strings.TrimRight(url, "/"), Kind: kind, APIKey: getenv(sharedKeyEnv), Normalize: true, Truncate: true}, false, nil
	}
	ep, err := resolveEndpoint(getenv(installation.EndpointsEnv), name, getenv, productionLike)
	if err != nil {
		return ep, false, err
	}
	if ep.Kind != kind {
		return ep, false, fmt.Errorf("endpoint %q is kind %q, want %q", name, ep.Kind, kind)
	}
	return ep, true, nil
}
