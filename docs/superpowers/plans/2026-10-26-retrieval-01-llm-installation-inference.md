# Retrieval PR 1 — `llm` installation-level inference Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the core `llm` module loads operator-declared inference endpoints from `RETRIEVAL_INFERENCE_ENDPOINTS`, verifies them against Text Embeddings Inference (TEI), and serves `iface.LLMInferenceProfiles`, `iface.LLMPinnedEmbedder` and `iface.LLMReranker` to the `retrieval` module only, with one ledger row and one budget reservation per call.

**Architecture:** a new leaf package `internal/core/llm/installation` parses the environment into `Endpoint`s, computes inference-profile IDs and keeps a verification cache (`Set`) that checks each endpoint's `GET /info` and measures embedding dimensions with a probe. A hand-written `providers.TEI` adapter talks to TEI over an HTTP client built **without** the netguard private-address guard; it is never registered in the org `providers.Registry` and `tei` is never in `models.Providers`, so no organization model can reach it. `services.InstallationService` validates, reserves through the existing `BudgetService`, calls TEI behind its own breaker and finalizes the ledger; the registered `*services.Gateway` delegates the three new interfaces to it. `LLMGateway`, the org catalog and the router's candidate logic are untouched.

**Tech Stack:** Go 1.26.10, `net/http` + `httptest`, `golang.org/x/sync/singleflight` (already in `go.mod`), MongoDB 8.0 replica set for the one gated repository test.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D4, and the environment part of D9) and the roadmap `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (row 1; section «SDK — `pkg/sdk/iface/llm_installation.go` (PR 1)», the environment block and the «Ledger» paragraph under it). Implement against the roadmap's contract verbatim.

## Global Constraints

Every task includes the roadmap's «Global constraints» section. In addition, for this PR:

- `backend/pkg/sdk/iface/llm_installation.go` is the roadmap block **verbatim** (package `iface`, imports `context` and `errors`). `iface.LLMGateway` gains nothing; the registered `*services.Gateway` implements the three new interfaces and consumers assert them on the resolved `LLMGateway`.
- The private-address guard stays as it is for every organization endpoint: `Module.clientFor` keeps `ProductionLike: m.prodLike`. Only the installation client (`providers.NewInstallationHTTPClient`) skips it, and it is only ever given URLs from `RETRIEVAL_INFERENCE_ENDPOINTS`. `tei` is never added to `models.Providers` and never registered in `providers.RegisterAll`.
- `llm` `Init` never contacts an endpoint. Every problem with `RETRIEVAL_INFERENCE_ENDPOINTS`, `RETRIEVAL_INFERENCE_API_KEY` or an `apiKeyEnv` variable is a WARN naming the field, never an `Init` error. An unset `RETRIEVAL_INFERENCE_ENDPOINTS` logs nothing.
- No API key value reaches a log line, an error, a warning, an `InferenceProfile` or a ledger row.
- Ledger values, verbatim from the roadmap: `caller = "retrieval"`, `purpose = "retrieval.embed" | "retrieval.rerank"`, `modelUuid = "installation:<endpoint>"`, `provider = "tei"`, `credentialKind = "installation"` (`models.CredentialKindInstallation`), billed to `OnBehalfOf`.
- **Rows without a user (decision).** `BudgetService.Reserve` (`services/budget.go:135-137`) stamps `UserUUID` from the input, else from `ctxauth.GetUserUUID(ctx)`, else leaves it empty, and `UsageEvent.UserUUID` is `omitempty`. Installation calls pass no user of their own: a search (user in context) is ledgered under the searcher; an indexing call from the worker (tenant in context, no user) is ledgered with **no `userUuid`**. Such a row is billed to the org, lands in the org counters of the rollup (`Monthly.Apply` treats every kind but `user_account` as org, `repository/usage.go:389-392`), appears under the empty key in the by-user report (`$ifNull` in `Usage.Query`), is untouched by the DSR queries (they filter on a non-empty `userUuid`), and is closed by the reconciler and purged by retention like any row. No change to `Reserve`, the repositories or the PII producer.
- No HTTP route, permission, Cedar policy, Mongo collection, index or `errcode` code is added. Compose wiring, `.env.example`, `init.sh` and `env-validate.sh` are PR 8; this PR documents the variables only.
- Commits use this shape (subject per step):

  ```bash
  git commit -F - <<'EOF'
  feat(llm): <subject>

  Prop: upstream
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  EOF
  ```

- Per-task verification: `cd backend && go test ./internal/core/llm/... ./pkg/sdk/iface/... ./pkg/sdk/metrics/...`. Before the PR: `make ci-backend` from the repo root, and the repository test with `MONGO_TEST_URI` set (it skips without it).

## Review Focus

1. **An endpoint replaced under a running profile** (same name, another model, revision or dimension count): `ErrLLMProfileMismatch`, never vectors from another space; a changed dimension count is caught on the very next call, a same-size swap at the next re-verification. Pinned in Task 4 (`TestSet_MismatchCases`) and Task 5 (`TestInstallation_EndpointSwappedUnderARunningProfile`).
2. **A batch larger than TEI's `max_client_batch_size`** (the gateway admits 256 inputs, TEI defaults to 32): split, input order kept, rerank indices global. Pinned in Task 2 (`TestTEI_EmbedSplitsAtMaxClientBatchSize`, `TestTEI_RerankMergesBatchesWithGlobalIndices`).
3. **An indexing call with no user in context**: one ledger row without `userUuid`, billed to the org; an `OnBehalfOf` other than the context tenant is refused before anything runs. Pinned in Task 5 (`TestInstallation_IndexingCallWithoutUserBillsTheOrg`, `TestInstallation_RefusesBeforeAnyCall`).
4. **TEI down or slow**: `ErrLLMProviderUnavailable` without hammering it — a failed verification is cached for the backoff, the breaker opens after three faults, and a caller that gives up never poisons the cache. Pinned in Task 4 (`TestSet_UnreachableIsCachedForTheBackoff`, `TestSet_CallerCancelDoesNotPoisonTheCache`) and Task 5 (`TestInstallation_UnavailableEndpointOpensTheBreaker`).
5. **An operator typo in the environment** (`"normalise"`, a duplicated name, an `apiKeyEnv` naming an unset variable, malformed JSON): that endpoint (or all, for malformed JSON) is absent with a WARN naming the field, the backend boots, the key never appears. Pinned in Task 3 (`TestParse_InvalidFieldDropsOnlyThatEndpoint`, `TestParse_WarningsNeverCarryTheKey`) and Task 6 (`TestModule_MalformedInferenceEndpointsWarnAndBoot`).

---

### Task 1: SDK contract and the `tei` metric label

**Files:**
- Create: `backend/pkg/sdk/iface/llm_installation.go`
- Create: `backend/pkg/sdk/iface/llm_installation_test.go`
- Modify: `backend/pkg/sdk/iface/llm_test.go:10-15` (sentinel list)
- Modify: `backend/pkg/sdk/metrics/metrics.go:626` (`llmProviders`)
- Modify: `backend/pkg/sdk/metrics/llm_test.go`
- Modify: `backend/pkg/sdk/AGENTS.md:49` (iface row), `docs/site/sdk/shared-iface.mdx:90`

**Interfaces:**
- Consumes: nothing.
- Produces: every type, constant, interface and `ErrLLMProfileMismatch` of the roadmap block, used by Tasks 3–6 and by PRs 3–5. The `provider` label of `orkestra_llm_requests_total` / `orkestra_llm_tokens_total` accepts `tei`.

- [ ] **Step 1: Write the failing tests**

`llm_installation_test.go` (these values are persisted by retrieval and typed by operators, so they are pinned):

```go
func TestInstallationConstants(t *testing.T) {
	for got, want := range map[string]string{
		string(InferenceKindEmbed): "embed", string(InferenceKindRerank): "rerank", string(InferenceKindChat): "chat",
		string(InferenceBoundaryLocal): "local", string(InferenceBoundaryHosted): "hosted",
		string(EmbedInputQuery): "query", string(EmbedInputDocument): "document",
	} {
		if got != want {
			t.Errorf("constant = %q, want %q", got, want)
		}
	}
}

func TestInferenceProfile_JSONShape(t *testing.T) {
	b, _ := json.Marshal(InferenceProfile{ID: "ip_x", Endpoint: "embed", Kind: InferenceKindEmbed, Boundary: InferenceBoundaryLocal, ModelID: "m", Revision: "r"})
	const want = `{"id":"ip_x","endpoint":"embed","kind":"embed","boundary":"local","modelId":"m","revision":"r","normalize":false,"truncate":false}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

func TestRerankResult_JSONShape(t *testing.T) {
	b, _ := json.Marshal(RerankResult{Results: []RerankScore{{Index: 1, Score: 0.5}}, Profile: "ip_x", Usage: LLMUsage{InputTokens: 3}})
	const want = `{"results":[{"index":1,"score":0.5}],"profile":"ip_x","usage":{"inputTokens":3,"outputTokens":0}}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}
```

In `llm_test.go`, add `ErrLLMProfileMismatch` to the `all` slice of `TestLLMSentinels_AreDistinctAndWrappable`.

In `metrics/llm_test.go`:

```go
func TestLLMMetrics_TEIIsAKnownProvider(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatal(err)
	}
	c.RecordLLMRequest("tei", "ok")
	c.RecordLLMTokens("tei", "input", 5)
	if got := testutil.ToFloat64(c.llmRequests.WithLabelValues("tei", "ok")); got != 1 {
		t.Fatalf("tei/ok = %v", got)
	}
	if got := testutil.ToFloat64(c.llmTokens.WithLabelValues("tei", "input")); got != 5 {
		t.Fatalf("tei/input = %v", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./pkg/sdk/iface/ ./pkg/sdk/metrics/`
Expected: FAIL — `undefined: InferenceProfile` (iface), `tei/ok = 0` (metrics).

- [ ] **Step 3: Implement**

Create `llm_installation.go` with the roadmap block verbatim. Add `"tei": {}` to `llmProviders` in `metrics.go:626`.

Docs: in `pkg/sdk/AGENTS.md:49` append to the narrow sub-interfaces list `LLMInferenceProfiles`, `LLMPinnedEmbedder` and `LLMReranker` (installation-level inference for the `retrieval` module, ADR-0023; asserted on the `ServiceLLMGateway` value, caller `retrieval` only) and the sentinel `ErrLLMProfileMismatch`. In `shared-iface.mdx:90` add one sentence: `pkg/sdk/iface/llm_installation.go` holds the three companion interfaces the same gateway implements for the core `retrieval` module, with the inference-profile types.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./pkg/sdk/iface/ ./pkg/sdk/metrics/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(sdk): add the installation inference contract (ADR-0023)`

---

### Task 2: TEI adapter, installation HTTP client and the TEI fake

**Files:**
- Create: `backend/internal/core/llm/providers/tei.go`
- Create: `backend/internal/core/llm/providers/tei_test.go`
- Create: `backend/internal/core/llm/providers/teitest/teitest.go`
- Modify: `backend/internal/core/llm/providers/transport.go` (new constructor after `NewHTTPClient`, ~L163)
- Modify: `backend/internal/core/llm/providers/transport_test.go`
- Modify: `backend/internal/core/llm/models/credential.go:9-19`, `backend/internal/core/llm/models/model.go:11-14`, `backend/internal/core/llm/models/model_test.go`

**Interfaces:**
- Consumes: `iface.RerankScore`, `iface.LLMUsage` (Task 1); `providers.EmbedOutcome`, `ReadAllBounded`, `WithResponseCap`, `MaxEmbedResponseBytes`, `transportErr`, `malformed`, `ErrUnauthorized` (existing).
- Produces:

```go
// models
const ProviderTEI = "tei"                       // NOT in models.Providers
const CredentialKindInstallation = "installation"

// providers
func NewInstallationHTTPClient(cfg TransportConfig) *http.Client // NewHTTPClient with ProductionLike forced false
const TEIDefaultBatchSize = 32
type TEI struct{ /* base URL, key, client */ }
func NewTEI(baseURL, apiKey string, client *http.Client) (*TEI, error)
type TEIInfo struct {
	ModelID            string       `json:"model_id"`
	ModelSHA           *string      `json:"model_sha"`
	ModelType          TEIModelType `json:"model_type"`
	MaxInputLength     int          `json:"max_input_length"`
	MaxClientBatchSize int          `json:"max_client_batch_size"`
}
type TEIModelType struct {
	Embedding  *TEIEmbeddingType `json:"embedding,omitempty"`
	Reranker   json.RawMessage   `json:"reranker,omitempty"`   // present = len > 0
	Classifier json.RawMessage   `json:"classifier,omitempty"`
}
type TEIEmbeddingType struct{ Pooling string `json:"pooling"` }
type TEIEmbedOptions struct{ Normalize, Truncate bool; BatchSize int } // BatchSize <= 0 → TEIDefaultBatchSize
type TEIRerankOptions struct{ Truncate bool; BatchSize int }
func (t *TEI) Info(ctx context.Context) (*TEIInfo, error)
// Embed: one vector per input, in order. Like Provider.Embed, a failure after
// completed batches returns a non-nil outcome whose Usage is those batches.
func (t *TEI) Embed(ctx context.Context, inputs []string, o TEIEmbedOptions) (*EmbedOutcome, error)
// Rerank: every text scored, Index global, ordered by Score desc then Index asc.
// The usage is valid (completed batches) even with an error.
func (t *TEI) Rerank(ctx context.Context, query string, texts []string, o TEIRerankOptions) ([]iface.RerankScore, iface.LLMUsage, error)

// providers/teitest (imported by tests only)
type Config struct {
	ModelID            string        // /info model_id
	Revision           string        // /info model_sha; "" → JSON null
	Kind               string        // "embed" (default) | "rerank"
	Pooling            string        // embed: model_type.embedding.pooling; default "cls"
	Dimensions         int           // embed vector length; default 4
	MaxInputLength     int           // default 8192
	MaxClientBatchSize int           // default 32; a larger batch answers 413
	APIKey             string        // set: every route needs "Authorization: Bearer <APIKey>", else 401
	OmitComputeTokens  bool          // do not send x-compute-tokens
	InfoDelay          time.Duration // sleep before answering /info
}
type EmbedCall struct{ Inputs []string; Normalize, Truncate bool }
type RerankCall struct{ Query string; Texts []string; Truncate bool }
type Server struct{ URL string /* + unexported state, mutex-guarded */ }
func New(t testing.TB, cfg Config) *Server            // httptest.Server closed on t.Cleanup
func (s *Server) SetConfig(cfg Config)                // the endpoint now serves another model
func (s *Server) Fail(path string, after, status int) // requests on path beyond the first `after` answer status with {"error":"injected","error_type":"Backend"}; status 0 clears
func (s *Server) InfoCalls() int
func (s *Server) EmbedCalls() []EmbedCall
func (s *Server) RerankCalls() []RerankCall
func (s *Server) AuthHeaders() []string
```

Fake outputs (deterministic, so tests can assert them): `/embed` vector `i` has `Dimensions` floats, `v[0] = float32(len(inputs[i]))`, the rest `0.5`, header `x-compute-tokens` = Σ `len(input)` in bytes; a `rerank`-kind server answers `/embed` with 400. `/rerank` scores `float32(len(text))/1000`, answers sorted by score desc, header `x-compute-tokens` = `len(query)*len(texts) + Σ len(text)`. `/info` `model_type` is `{"embedding":{"pooling":<Pooling>}}` or `{"reranker":{"id2label":{"0":"LABEL_0"}}}`.

- [ ] **Step 1: Write the failing tests**

`models/model_test.go`:

```go
func TestProviders_TEIIsNotAnOrgProvider(t *testing.T) {
	if IsProvider(ProviderTEI) {
		t.Fatal("tei must never be selectable for an organization credential or model")
	}
	if CredentialKindInstallation != "installation" {
		t.Fatalf("CredentialKindInstallation = %q", CredentialKindInstallation)
	}
}
```

`transport_test.go`:

```go
func TestInstallationHTTPClient_ReachesPrivateHostWhileTheOrgClientCannot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	org := NewHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	if _, err := org.Get(srv.URL); !errors.Is(err, ErrDestinationNotPublic) {
		t.Fatalf("org client err = %v, want ErrDestinationNotPublic", err)
	}
	inst := NewInstallationHTTPClient(TransportConfig{ProductionLike: true, Timeout: 2 * time.Second})
	resp, err := inst.Get(srv.URL)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("installation client = %v, %v", resp, err)
	}
	resp.Body.Close()
}

func TestInstallationHTTPClient_KeepsTheOtherProtections(t *testing.T) {
	redirect := httptest.NewServer(http.RedirectHandler("http://example.test/", http.StatusFound))
	defer redirect.Close()
	if _, err := NewInstallationHTTPClient(TransportConfig{}).Get(redirect.URL); !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("err = %v, want ErrRedirectRefused", err)
	}
}
```

`tei_test.go` (all against `teitest.New`, client `NewInstallationHTTPClient(TransportConfig{Timeout: 5 * time.Second})`):

```go
func TestTEI_InfoParsesIdentity(t *testing.T) {
	// cfg: ModelID "BAAI/bge-m3", Revision "abc123", Pooling "cls", MaxInputLength 8192, MaxClientBatchSize 32
	// want: info.ModelID == "BAAI/bge-m3"; *info.ModelSHA == "abc123";
	//       info.ModelType.Embedding.Pooling == "cls"; len(info.ModelType.Reranker) == 0;
	//       info.MaxInputLength == 8192; info.MaxClientBatchSize == 32
	// Revision "" → info.ModelSHA == nil; Kind "rerank" → Embedding == nil, len(Reranker) > 0
}

func TestTEI_EmbedSplitsAtMaxClientBatchSize(t *testing.T) {
	// 70 inputs, inputs[i] = strings.Repeat("x", i+1); options {Normalize: true, Truncate: true, BatchSize: 32}
	// want: EmbedCalls() lengths [32 32 6], every call Normalize && Truncate;
	//       len(out.Vectors) == 70 and out.Vectors[i][0] == float32(i+1) for every i;
	//       out.Usage.InputTokens == Σ(i+1) for i in 0..69 == 2485 (from x-compute-tokens)
}

func TestTEI_EmbedWithoutComputeTokensEstimates(t *testing.T) {
	// OmitComputeTokens; inputs ["abcd", "ef"] → Usage.InputTokens == (6+2)/3 == 2
}

func TestTEI_RerankMergesBatchesWithGlobalIndices(t *testing.T) {
	// 40 texts, texts[i] = strings.Repeat("y", (i*7)%40+1) (distinct lengths), BatchSize 16
	// want: RerankCalls() lengths [16 16 8], each with Query "q";
	//       40 scores, every Index in 0..39 exactly once;
	//       scores[k].Score >= scores[k+1].Score; scores[0].Index is the longest text's index
}

func TestTEI_FailedLaterBatchReportsCompletedUsage(t *testing.T) {
	// srv.Fail("/embed", 1, 503); 40 inputs of "ab", BatchSize 32
	// want: errors.Is(err, iface.ErrLLMProviderUnavailable); out != nil; out.Usage.InputTokens == 64
}

func TestTEI_SendsBearerOnlyWhenKeySet(t *testing.T) {
	// APIKey "k1", NewTEI(url, "k1", c): Info, Embed, Rerank all succeed; every AuthHeaders() == "Bearer k1"
	// NewTEI(url, "", c) against APIKey "k1": Info → errors.Is(err, ErrUnauthorized)
	// against a keyless fake with key "": AuthHeaders() all ""
}

func TestTEI_ClassifiesStatusesWithoutProviderText(t *testing.T) {
	for status, want := range map[int]error{
		401: ErrUnauthorized, 403: ErrUnauthorized,
		400: iface.ErrLLMInvalidRequest, 413: iface.ErrLLMInvalidRequest, 422: iface.ErrLLMInvalidRequest,
		404: iface.ErrLLMProviderUnavailable, 408: iface.ErrLLMProviderUnavailable, 424: iface.ErrLLMProviderUnavailable,
		429: iface.ErrLLMProviderUnavailable, 503: iface.ErrLLMProviderUnavailable,
	} {
		// srv.Fail("/embed", 0, status); _, err := tei.Embed(ctx, []string{"a"}, TEIEmbedOptions{})
		// want errors.Is(err, want); 401/403 also errors.Is(err, iface.ErrLLMProviderUnavailable);
		// strings.Contains(err.Error(), "injected") == false
	}
}

func TestTEI_RefusesMalformedResponses(t *testing.T) {
	// inline httptest servers:
	//   /embed answers [[0.1]] for 2 inputs           → errors.Is(err, iface.ErrLLMProviderUnavailable)
	//   /embed answers [[0.1],[]]                      → same
	//   /rerank answers [{"index":0},{"index":0}]      → same (duplicate index)
	//   /rerank answers [{"index":5,"score":1}] for 1  → same (out of range)
	//   /info without model_id                         → same
}

func TestNewTEI_RequiresClientAndURL(t *testing.T) {
	// NewTEI("http://tei:80", "", nil) → error; NewTEI("", "", c) → errors.Is(err, iface.ErrLLMInvalidRequest)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/llm/providers/ ./internal/core/llm/models/`
Expected: FAIL — `undefined: NewInstallationHTTPClient`, `undefined: teitest`, `undefined: ProviderTEI`.

- [ ] **Step 3: Implement**

- `models`: the two constants; `models.Providers` unchanged.
- `NewInstallationHTTPClient(cfg TransportConfig) *http.Client` sets `cfg.ProductionLike = false` and returns `NewHTTPClient(cfg)`. Its doc comment states it is for operator-declared endpoints only and must never receive a URL that came from the database or the API.
- `teitest`: an `httptest.Server` with the behaviour of the Interfaces block.
- `providers.TEI` (follow `ollama.go`'s `newRequest`/`send` shape): `Authorization: Bearer <key>` on every request when the key is non-empty; `GET /info` read with `ReadAllBounded(…, 1<<20)`; `POST /embed` body `{"inputs":[…],"normalize":b,"truncate":b}` under `WithResponseCap(ctx, MaxEmbedResponseBytes)`; `POST /rerank` body `{"query":q,"texts":[…],"truncate":b,"raw_scores":false,"return_text":false}`. Batches of `BatchSize`, in order; per-batch usage is the positive integer in `x-compute-tokens`, else `ceil(bytes/3)` of the batch (embed: Σ input bytes; rerank: `len(query)*len(batch) + Σ text bytes`). A status ≥ 400 drains at most 4 KiB of the body and returns `classifyTEI(status)`:

| Status | Error |
|---|---|
| 401, 403 | `fmt.Errorf("%w: %w", ErrUnauthorized, iface.ErrLLMProviderUnavailable)` |
| 400, 413, 422 | `iface.ErrLLMInvalidRequest` |
| anything else ≥ 400 (404, 408, 424, 429, 5xx) | `iface.ErrLLMProviderUnavailable` |

The wrapped detail carries the status only. Transport failures go through `transportErr`; shape errors through `malformed(...)`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/core/llm/providers/... ./internal/core/llm/models/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(llm): add the TEI adapter and the installation HTTP client`

---

### Task 3: Installation endpoints from the environment and the profile ID

**Files:**
- Create: `backend/internal/core/llm/installation/config.go`
- Create: `backend/internal/core/llm/installation/profile.go`
- Create: `backend/internal/core/llm/installation/config_test.go`
- Create: `backend/internal/core/llm/installation/profile_test.go`

**Interfaces:**
- Consumes: `iface.InferenceProfile`, `iface.InferenceKind*`, `iface.InferenceBoundary*` (Task 1).
- Produces:

```go
package installation

const (
	EndpointsEnv = "RETRIEVAL_INFERENCE_ENDPOINTS"
	APIKeyEnv    = "RETRIEVAL_INFERENCE_API_KEY"
)

type Endpoint struct {
	Name           string
	URL            string // scheme://host[:port][/path], no trailing slash
	Kind           iface.InferenceKind
	Boundary       iface.InferenceBoundary
	Model          string
	Revision       string
	Pooling        string
	Normalize      bool
	Truncate       bool
	QueryPrefix    string
	DocumentPrefix string
	APIKey         string // resolved from the environment; never logged, never in a Profile
}

// Profile: the declared fields plus ID; Dimensions and MaxInputTokens are zero
// (they are measured at verification, Task 4).
func (e Endpoint) Profile() iface.InferenceProfile
func (e Endpoint) LedgerModelUUID() string // "installation:" + Name

func ProfileID(p iface.InferenceProfile) string

// Parse never fails: an invalid endpoint is dropped and described in the
// returned warnings (one line each, no key value). raw "" or blank → nil, nil.
func Parse(raw string, getenv func(string) string, productionLike bool) ([]Endpoint, []string)
```

`ProfileID` hashes exactly this value with `encoding/json` (field order as written, no `omitempty`) and returns `"ip_" + hex(sha256(bytes))`:

```go
type profileIdentity struct {
	Kind           iface.InferenceKind `json:"kind"`
	ModelID        string              `json:"modelId"`
	Revision       string              `json:"revision"`
	Pooling        string              `json:"pooling"`
	Normalize      bool                `json:"normalize"`
	Truncate       bool                `json:"truncate"`
	QueryPrefix    string              `json:"queryPrefix"`
	DocumentPrefix string              `json:"documentPrefix"`
}
```

Element JSON keys: `name`, `url`, `kind`, `boundary`, `model`, `revision`, `pooling`, `normalize`, `truncate`, `queryPrefix`, `documentPrefix`, `apiKeyEnv`. Validation, per element (first failing rule is the WARN's field):

| Field | Rule |
|---|---|
| (any key) | unknown key refused: decode each element with `DisallowUnknownFields` |
| `name` | `^[a-z][a-z0-9_-]{0,31}$` |
| `url` | `http` or `https`, non-empty host, no userinfo, query or fragment; `boundary: hosted` with `http` refused when `productionLike` |
| `kind` | `embed`, `rerank`, `chat` |
| `boundary` | `local`, `hosted` — required, never inferred |
| `model` | `^[A-Za-z0-9/][A-Za-z0-9._/:-]{0,199}$` |
| `revision` | `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$` (required) |
| `pooling` | embed: `cls`, `mean` or `last_token` (required); rerank, chat: empty |
| `normalize` | rerank, chat: `false` |
| `truncate` | chat: `false` |
| `queryPrefix`, `documentPrefix` | embed: valid UTF-8, ≤ 256 bytes; rerank, chat: empty |
| `apiKeyEnv` | `^RETRIEVAL_INFERENCE_[A-Z0-9_]{1,64}$` and the variable non-empty; absent → `RETRIEVAL_INFERENCE_API_KEY` (empty = no `Authorization` header) |

After per-element validation: every endpoint sharing a duplicated `name` is dropped, and every endpoint sharing a duplicated profile ID is dropped (one WARN each), because a call names a profile and must reach exactly one endpoint. A top level that is not a JSON array of objects drops everything with one WARN. Warning formats: `RETRIEVAL_INFERENCE_ENDPOINTS[<i>]: <field>: <reason>; endpoint ignored`, `RETRIEVAL_INFERENCE_ENDPOINTS: duplicate name "<name>"; every endpoint with this name ignored`, `RETRIEVAL_INFERENCE_ENDPOINTS: duplicate profile <id> (endpoints "<a>", "<b>"); all ignored`, `RETRIEVAL_INFERENCE_ENDPOINTS is malformed JSON (want an array of objects); no installation endpoint loaded`.

- [ ] **Step 1: Write the failing tests**

`profile_test.go`:

```go
const (
	embedID  = "ip_089b7e60b3d89b3c948d5b17d9e42d5008999b8f173f6da98d433d6db8027323"
	rerankID = "ip_f24b9aabce34b8f83c405595db0b73f005db166274d15b058bfcd2fc424d9fb6"
)

func TestProfileID_Golden(t *testing.T) {
	embed := iface.InferenceProfile{Kind: iface.InferenceKindEmbed, ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Normalize: true, Truncate: true}
	rerank := iface.InferenceProfile{Kind: iface.InferenceKindRerank, ModelID: "BAAI/bge-reranker-v2-m3", Revision: "def456", Truncate: true}
	if got := ProfileID(embed); got != embedID {
		t.Fatalf("embed = %s", got)
	}
	if got := ProfileID(rerank); got != rerankID {
		t.Fatalf("rerank = %s", got)
	}
}

func TestProfileID_IgnoresNameURLBoundaryAndMeasuredFields(t *testing.T) {
	// base = the embed profile above; same identity with Endpoint "other", Boundary hosted,
	// Dimensions 1024, MaxInputTokens 8192, ID "x" → ProfileID == embedID
}

func TestProfileID_EveryIdentityFieldChangesIt(t *testing.T) {
	// mutate each of Kind, ModelID, Revision, Pooling, Normalize, Truncate, QueryPrefix, DocumentPrefix
	// in turn → ProfileID != embedID; every ID matches ^ip_[0-9a-f]{64}$
}
```

`config_test.go` (`env := func(m map[string]string) func(string) string { … }`):

```go
const roadmapExample = `[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"abc123","pooling":"cls","normalize":true,"truncate":true},{"name":"rerank","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"BAAI/bge-reranker-v2-m3","revision":"def456","truncate":true}]`

func TestParse_RoadmapExample(t *testing.T) {
	eps, warns := Parse(roadmapExample, env(map[string]string{APIKeyEnv: "shared-key"}), true)
	// want: len(warns) == 0; len(eps) == 2;
	// eps[0] == Endpoint{Name: "embed", URL: "http://tei-embed:80", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal,
	//   Model: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Normalize: true, Truncate: true, APIKey: "shared-key"};
	// eps[0].Profile().ID == embedID; eps[1].Profile().ID == rerankID; eps[0].LedgerModelUUID() == "installation:embed";
	// eps[0].Profile().Endpoint == "embed", .Boundary == local, .Dimensions == 0
}

func TestParse_EmptyIsSilent(t *testing.T)                  // "" and "  \n" → nil, nil
func TestParse_MalformedJSONDropsEverything(t *testing.T)   // `[{"name":` and `{"name":"x"}` → 0 endpoints, 1 warning containing "malformed"

func TestParse_InvalidFieldDropsOnlyThatEndpoint(t *testing.T) {
	// each case: `[<bad>, <valid rerank element>]` → exactly the rerank endpoint, and a warning containing field
	cases := []struct{ field, bad string }{
		{"name", `{"name":"Embed!", …valid embed fields}`},
		{"url", `url "ftp://x"`}, {"url", `url "http://u:p@h"`}, {"url", `url "http://h/?q=1"`},
		{"kind", `kind "classify"`}, {"boundary", `boundary omitted`},
		{"model", `model ""`}, {"revision", `revision omitted`},
		{"pooling", `embed, pooling omitted`}, {"pooling", `embed, pooling "splade"`},
		{"pooling", `rerank, pooling "cls"`}, {"normalize", `rerank, normalize true`},
		{"queryPrefix", `rerank, queryPrefix "q: "`}, {"queryPrefix", `embed, queryPrefix of 257 bytes`},
		{"truncate", `chat, truncate true`},
		{"normalise", `embed with the extra key "normalise": true`},
		{"apiKeyEnv", `apiKeyEnv "HOME"`}, {"apiKeyEnv", `apiKeyEnv "RETRIEVAL_INFERENCE_KEY_EMBED", unset`},
	}
}

func TestParse_HostedHTTPRefusedOnlyWhenProductionLike(t *testing.T) {
	// hosted + http: productionLike true → 0 endpoints, warning contains "url"; false → 1 endpoint
	// local + http + productionLike true → 1 endpoint; hosted + https + productionLike → 1 endpoint
}

func TestParse_DuplicateNameDropsEveryHolder(t *testing.T)    // embed, embed (other model), rerank → [rerank]; warning contains `duplicate name "embed"`
func TestParse_DuplicateProfileDropsEveryHolder(t *testing.T) // "embed-a", "embed-b" with identical identity → none; warning contains "duplicate profile"

func TestParse_PerEndpointKey(t *testing.T) {
	// rerank element with "apiKeyEnv":"RETRIEVAL_INFERENCE_API_KEY_RERANK"; env shared "k1", rerank "k2"
	// → eps[0].APIKey == "k1", eps[1].APIKey == "k2"
}

func TestParse_WarningsNeverCarryTheKey(t *testing.T) {
	// env shared "sekrit-123"; one invalid element of each kind above plus the roadmap example
	// → strings.Contains(strings.Join(warns, "\n"), "sekrit-123") == false
}

func TestParse_ChatIsParsed(t *testing.T) // kind chat, boundary local, model, revision, nothing else → 1 endpoint, Kind chat
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/llm/installation/`
Expected: FAIL — package does not exist / `undefined: Parse`.

- [ ] **Step 3: Implement** `Parse`, `ProfileID`, `Endpoint.Profile`, `Endpoint.LedgerModelUUID` in the two files, with the rules and warning formats above. Decode the top level into `[]json.RawMessage`, each element with its own `json.Decoder` + `DisallowUnknownFields`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test ./internal/core/llm/installation/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(llm): parse installation inference endpoints from the environment`

---

### Task 4: Verification against `/info` with a cache

**Files:**
- Create: `backend/internal/core/llm/installation/set.go`
- Create: `backend/internal/core/llm/installation/set_test.go`

**Interfaces:**
- Consumes: `Endpoint`, `ProfileID` (Task 3); `providers.TEI`, `providers.NewTEI`, `providers.TEIEmbedOptions`, `providers.TEIDefaultBatchSize`, `teitest` (Task 2); `iface.ErrLLMProfileMismatch`, `ErrLLMNotConfigured`, `ErrLLMProviderUnavailable`.
- Produces:

```go
const (
	DefaultVerifyInterval = time.Minute       // a positive verification is reused this long
	DefaultFailureBackoff = 5 * time.Second   // a failed verification is returned again this long
	DefaultVerifyTimeout  = 10 * time.Second  // one verification, detached from the caller
	ProbeText             = "orkestra inference probe"
)

type Options struct {
	VerifyInterval, FailureBackoff, VerifyTimeout time.Duration // zero → defaults
	Now func() time.Time                                         // nil → time.Now
}

type Verified struct {
	Profile   iface.InferenceProfile // declared fields + ID + Dimensions (embed) + MaxInputTokens
	BatchSize int                    // max_client_batch_size, TEIDefaultBatchSize when <= 0
}

type Set struct{ /* immutable endpoint map + per-endpoint cache, singleflight.Group */ }
func NewSet(eps []Endpoint, client *http.Client, opts Options) *Set
func (s *Set) Len() int
func (s *Set) Endpoint(name string) (Endpoint, bool)
func (s *Set) ByProfile(id string) (Endpoint, bool)
func (s *Set) HasKind(k iface.InferenceKind) bool
func (s *Set) Adapter(name string) *providers.TEI
func (s *Set) Verify(ctx context.Context, name string) (*Verified, error) // returns a copy
func (s *Set) Invalidate(name string)
```

`Verify(ctx, name)`:

1. unknown name → `fmt.Errorf("%w: unknown inference endpoint", iface.ErrLLMNotConfigured)`; kind `chat` → `ErrLLMNotConfigured` ("chat endpoints are served from delivery step 3").
2. A cached success younger than `VerifyInterval` is returned; a cached failure younger than `FailureBackoff` is returned again.
3. Otherwise one verification per endpoint at a time (`singleflight.DoChan` keyed by name) on `context.WithoutCancel(ctx)` bounded by `VerifyTimeout`; the caller waits on the channel or `ctx.Done()` (then returns `ctx.Err()` and the flight still completes and fills the cache).
4. The verification: `Info`; then `model_id == Model`, `model_sha != nil && *model_sha == Revision`, embed ⇒ `model_type.embedding != nil` and its `pooling == Pooling`, rerank ⇒ `len(model_type.reranker) > 0`; any difference → `fmt.Errorf("%w: <field>", iface.ErrLLMProfileMismatch)` with field one of `model_id`, `model_sha`, `model_type`, `pooling`. Embed only: probe `Embed(ctx, []string{ProbeText}, TEIEmbedOptions{Normalize, Truncate, BatchSize: 1})`; `Dimensions = len(vector)`; a previously verified non-zero `Dimensions` that differs → `ErrLLMProfileMismatch` (`dimensions`). Adapter errors pass through (they are `ErrLLMProviderUnavailable`/`ErrUnauthorized`/…).
5. The outcome (success or failure) is cached with the time from `Now`. `Invalidate` clears the cached success but keeps the last measured dimensions (so a dimension change is still detected).

- [ ] **Step 1: Write the failing tests**

All use `teitest.New`, `providers.NewInstallationHTTPClient(providers.TransportConfig{})` and a manual clock (`type clock struct{ mu sync.Mutex; t time.Time }` with `Now`/`Advance`). `embedEP(url)` builds the Task 3 roadmap `embed` endpoint pointing at the fake.

```go
func TestSet_VerifiesAndMeasuresDimensions(t *testing.T) {
	// fake: ModelID "BAAI/bge-m3", Revision "abc123", Pooling "cls", Dimensions 1024, MaxInputLength 8192, MaxClientBatchSize 32
	// want v.Profile.ID == embedID, .Dimensions == 1024, .MaxInputTokens == 8192, .Endpoint == "embed", .Boundary == local;
	//      v.BatchSize == 32; EmbedCalls() == [{Inputs: [ProbeText], Normalize: true, Truncate: true}]
}

func TestSet_CachesUntilVerifyInterval(t *testing.T) {
	// Verify, Verify → InfoCalls() == 1; clock.Advance(61 * time.Second); Verify → InfoCalls() == 2
}

func TestSet_MismatchCases(t *testing.T) {
	// first Verify succeeds on the base fake, then srv.SetConfig(<change>), clock.Advance(61s), Verify:
	cases := map[string]teitest.Config{
		"model":      {ModelID: "BAAI/other", Revision: "abc123", Pooling: "cls"},
		"revision":   {ModelID: "BAAI/bge-m3", Revision: "zzz", Pooling: "cls"},
		"no revision": {ModelID: "BAAI/bge-m3", Revision: "", Pooling: "cls"},
		"pooling":    {ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "mean"},
		"kind":       {ModelID: "BAAI/bge-m3", Revision: "abc123", Kind: "rerank"},
		"dimensions": {ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Dimensions: 768},
	}
	// want errors.Is(err, iface.ErrLLMProfileMismatch) for each
}

func TestSet_UnreachableIsCachedForTheBackoff(t *testing.T) {
	// srv.Fail("/info", 0, 503): Verify → ErrLLMProviderUnavailable; again at +1s → same error, InfoCalls() == 1;
	// srv.Fail("/info", 0, 0); clock.Advance(6s); Verify → success, InfoCalls() == 2
}

func TestSet_ConcurrentVerifyRunsOnce(t *testing.T) {
	// InfoDelay 100ms; 20 goroutines Verify concurrently → all succeed, InfoCalls() == 1
}

func TestSet_CallerCancelDoesNotPoisonTheCache(t *testing.T) {
	// InfoDelay 200ms; ctx with 10ms timeout → errors.Is(err, context.DeadlineExceeded);
	// wait 300ms; Verify(context.Background()) → success, InfoCalls() == 1
}

func TestSet_UnknownAndChatAreNotConfigured(t *testing.T)  // Verify("nope"), Verify on a chat endpoint → ErrLLMNotConfigured, InfoCalls() == 0
func TestSet_RerankVerifiesWithoutProbe(t *testing.T)      // rerank fake/endpoint → success, Dimensions == 0, len(EmbedCalls()) == 0
func TestSet_InvalidateForcesReverify(t *testing.T)        // Verify, Invalidate, Verify → InfoCalls() == 2
func TestSet_LookupHelpers(t *testing.T)                   // ByProfile(embedID) → "embed"; ByProfile("ip_"+64 zeros) → false; HasKind(rerank) false with only embed; Len()
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/llm/installation/ -run TestSet`
Expected: FAIL — `undefined: NewSet`.

- [ ] **Step 3: Implement** `set.go` as specified.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test -race ./internal/core/llm/installation/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(llm): verify installation endpoints against TEI /info`

---

### Task 5: `InstallationService` and the gateway's three interfaces

**Files:**
- Create: `backend/internal/core/llm/services/installation.go`
- Create: `backend/internal/core/llm/services/installation_test.go`
- Modify: `backend/internal/core/llm/services/gateway.go` (struct L44-58, setters ~L96, assertions L67-71)
- Modify: `backend/internal/core/llm/services/router.go:122-149` (`errorCodeOf`), `router.go:487-501` (`finish`)
- Modify: `backend/internal/core/llm/repository/budget_txn_test.go`

**Interfaces:**
- Consumes: `installation.Set`, `Verified`, `Endpoint.LedgerModelUUID` (Tasks 3–4); `providers.TEI.Embed/Rerank` (Task 2); `models.ProviderTEI`, `models.CredentialKindInstallation` (Task 2); existing `BudgetService.Reserve/Finalize`, `ReserveInput`, `FinalizeInput`, `Breaker`, `RouterConfig`, `LLMMetrics`, `EstimateEmbedTokens`, `maxEmbedInputs`, `maxEmbedInputBytes`.
- Produces (PR 3–5 consume only the iface side, through the registered gateway):

```go
const (
	InstallationCaller     = "retrieval"
	PurposeRetrievalEmbed  = "retrieval.embed"
	PurposeRetrievalRerank = "retrieval.rerank"
)

type InstallationService struct{ /* set, budget, breaker, cfg, metrics, logger */ }
func NewInstallationService(set *installation.Set, budget *BudgetService, breaker *Breaker, cfg func() RouterConfig, metrics LLMMetrics, logger *slog.Logger) *InstallationService
func ValidatePinnedEmbedRequest(req iface.PinnedEmbedRequest) error
func ValidateRerankRequest(req iface.RerankRequest) error
func (s *InstallationService) InferenceProfile(ctx context.Context, endpoint string) (*iface.InferenceProfile, error)
func (s *InstallationService) EmbedWithProfile(ctx context.Context, req iface.PinnedEmbedRequest) (*iface.EmbedResult, error)
func (s *InstallationService) Rerank(ctx context.Context, req iface.RerankRequest) (*iface.RerankResult, error)

func (g *Gateway) SetInstallation(s *InstallationService)
func (g *Gateway) InferenceProfile(ctx context.Context, endpoint string) (*iface.InferenceProfile, error) // nil service → ErrLLMNotConfigured
func (g *Gateway) EmbedWithProfile(ctx context.Context, req iface.PinnedEmbedRequest) (*iface.EmbedResult, error)
func (g *Gateway) Rerank(ctx context.Context, req iface.RerankRequest) (*iface.RerankResult, error)

// router.go: Router.finish's body moves here unchanged; Router.finish calls it.
func finalizeCall(ctx context.Context, budget *BudgetService, metrics LLMMetrics, logger *slog.Logger, res *Reservation, attempts []models.Attempt, in FinalizeInput)
```

For PR 4/5 (consumers): every call needs a context carrying the tenant **and** tenant kind `internal` (`ctxauth.WithTenantKind`, as `ctxWithTenant` in `services/reconciler.go:92` does), `OnBehalfOf` equal to that tenant, and `Caller: "retrieval"`. Error mapping is in the table below.

**Validation** (before anything else; no ledger row, no TEI call):

| Request | Rule | Error |
|---|---|---|
| both | `Caller == "retrieval"` | `iface.ErrLLMModelAccessDenied` |
| both | `OnBehalfOf != ""`; `Profile` matches `^ip_[0-9a-f]{64}$` | `iface.ErrLLMInvalidRequest` |
| embed | `Kind` is `query` or `document`; 1–`maxEmbedInputs` inputs, each 1–`maxEmbedInputBytes` bytes | `iface.ErrLLMInvalidRequest` |
| rerank | `Query` 1–`maxEmbedInputBytes` bytes; 1–`maxEmbedInputs` documents, each 1–`maxEmbedInputBytes`; `0 <= TopN <= len(Documents)` | `iface.ErrLLMInvalidRequest` |

**Call path** of `EmbedWithProfile` (and `Rerank`, with the rerank kind and purpose):

1. validate (table above);
2. `tenantrepo.RequireInternalTenant(ctx)`; `tenantrepo.CurrentTenantID(ctx) == req.OnBehalfOf`, else `fmt.Errorf("%w: onBehalfOf is not the tenant in context", iface.ErrLLMInvalidRequest)`;
3. no endpoint of this kind in the set → `ErrLLMNotConfigured`; `set.ByProfile(req.Profile)` not found → `ErrLLMProfileMismatch`; found with another kind → `ErrLLMInvalidRequest`;
4. `set.Verify(ctx, name)` (its errors returned as they are; the verified `ID` always equals the declared one);
5. `breaker.Allow("installation:"+name)` (open → its error, no row);
6. `budget.Reserve(ctx, ReserveInput{ModelUUID: ep.LedgerModelUUID(), Provider: "tei", ModelID: ep.Model, CredentialKind: "installation", Caller: "retrieval", Purpose: <purpose>, EstimatedInput: <estimate>, ReserveOutput: 1})` — `UserUUID` empty on purpose (the context user, if any, is stamped by `Reserve`); a refusal releases the ticket, records metric `tei/budget_blocked` and returns `ErrLLMBudgetExceeded`; any other reserve error records `tei/error`;
7. the TEI call under `context.WithTimeout(ctx, cfg().RequestTimeout)` (`<= 0` → 60 s): embed with inputs prefixed by `QueryPrefix` (kind `query`) or `DocumentPrefix` (kind `document`), `TEIEmbedOptions{Normalize, Truncate, BatchSize: verified.BatchSize}`; rerank with `TEIRerankOptions{Truncate, BatchSize}`, then cut to `TopN` (0 = all);
8. embed: every vector must have `verified.Profile.Dimensions` elements, else the call's error is `ErrLLMProfileMismatch` (`dimensions`) and `set.Invalidate(name)`;
9. breaker: `Release` when the caller's context ended or the error is a profile mismatch, otherwise `Record(err)`;
10. `finalizeCall` with one attempt `{Provider: "tei", CredentialKind: "installation", Outcome: ok|error, InputTokens: <usage>}`, `Outcome` `ok`/`error`, `ErrorCode: errorCodeOf(err)`, the same identity fields as the reservation; metric `RecordLLMRequest("tei", outcome)` (tokens are recorded by `finalizeCall`).

Estimates: embed `EstimateEmbedTokens(prefixedInputs)`; rerank `(len(query)*len(docs) + Σ len(doc) + 2) / 3`. Results: `EmbedResult{Vectors, Dimensions, Usage, Model: iface.LLMModelInfo{UUID: "installation:<name>", Name: <name>, Provider: "tei", ModelID: <model>, Capabilities: iface.LLMCapabilities{Embeddings: true, Dimensions: <dims>}, CredentialKind: "installation", Purposes: []string{"retrieval.embed"}}}`; `RerankResult{Results, Profile: req.Profile, Usage}`. `errorCodeOf` gains `case errors.Is(err, iface.ErrLLMProfileMismatch): return "profile_mismatch"` before the default.

`InferenceProfile(ctx, endpoint)` needs no tenant (retrieval resolves profiles outside a request): `set.Verify` and a copy of `Profile`.

- [ ] **Step 1: Write the failing tests**

`installation_test.go` helper: `newInstallationEnv(t, embedCfg, rerankCfg teitest.Config, store *memBudgetStore)` → two fakes, the roadmap endpoints (Task 3) with `DocumentPrefix "passage: "`, `QueryPrefix "query: "`, a `Set` on a manual clock, `BudgetService` over `store` (nil budget unless given), `NewBreaker(3, 30*time.Second)`, `memMetrics`, and `g := newGatewayForTest(t); g.SetInstallation(svc)`. Fakes: embed `{ModelID "BAAI/bge-m3", Revision "abc123", Pooling "cls", Dimensions 4}`, rerank `{ModelID "BAAI/bge-reranker-v2-m3", Revision "def456", Kind "rerank"}`. `embedReq()` = `{Caller "retrieval", OnBehalfOf "t1", Profile embedID, Kind document, Inputs ["a","bb"]}`.

```go
func TestGateway_ImplementsTheInstallationInterfaces(t *testing.T) {
	var gw iface.LLMGateway = newGatewayForTest(t)
	_, a := gw.(iface.LLMInferenceProfiles)
	_, b := gw.(iface.LLMPinnedEmbedder)
	_, c := gw.(iface.LLMReranker)
	// want a && b && c; and without SetInstallation each method → errors.Is(err, iface.ErrLLMNotConfigured)
}

func TestInstallation_EmbedWithProfileLedgersOneRowPerCall(t *testing.T) {
	// ctxFor("t1") (user u-admin); g.EmbedWithProfile(ctx, embedReq())
	// want: len(Vectors) == 2, Dimensions == 4;
	//       fake embed calls after the probe == [{Inputs: ["passage: a", "passage: bb"], Normalize: true, Truncate: true}];
	//       res.Model == iface.LLMModelInfo{UUID: "installation:embed", Name: "embed", Provider: "tei", ModelID: "BAAI/bge-m3",
	//         Capabilities: iface.LLMCapabilities{Embeddings: true, Dimensions: 4}, CredentialKind: "installation", Purposes: []string{"retrieval.embed"}};
	//       row := singleLedger(t, store): Caller "retrieval", Purpose "retrieval.embed", ModelUUID "installation:embed", Provider "tei",
	//         ModelID "BAAI/bge-m3", CredentialKind "installation", CredentialUUID "", UserUUID "u-admin", TenantID "t1", Outcome "ok",
	//         InputTokens 21 (len("passage: a")+len("passage: bb")), Attempts [{tei installation ok 21 0}];
	//       store.month(period).OrgInputTokens == 21; metrics.requests["tei/ok"] == 1
	// Kind query with Inputs ["a"] → recorded input "query: a"
}

func TestInstallation_IndexingCallWithoutUserBillsTheOrg(t *testing.T) {
	// ctx := ctxWithTenant(context.Background(), "t1")  (tenant + kind internal, no user)
	// want: success; row.UserUUID == ""; row.TenantID == "t1"; OrgInputTokens > 0
}

func TestInstallation_RefusesBeforeAnyCall(t *testing.T) {
	// each case: errors.Is(err, want); no ledger row; fake InfoCalls() == 0 and no /embed or /rerank call
	// embed: Caller "crm" → ErrLLMModelAccessDenied; Caller "" → ErrLLMModelAccessDenied;
	//        OnBehalfOf "t2" under ctxFor("t1") → ErrLLMInvalidRequest; ctxauthExternal(t) with OnBehalfOf "ext-1" → tenantrepo.ErrTenantKindMismatch;
	//        Profile "bogus" → invalid; Kind "" → invalid; Inputs nil, 257 inputs, one input of 64<<10+1 bytes, an empty input → invalid
	// rerank: Query "", Documents nil, 257 documents, TopN -1, TopN len+1 → ErrLLMInvalidRequest
}

func TestInstallation_ProfileResolution(t *testing.T) {
	// Rerank with Profile embedID → ErrLLMInvalidRequest
	// EmbedWithProfile with Profile "ip_"+strings.Repeat("0", 64) → ErrLLMProfileMismatch
	// a service over an empty Set: EmbedWithProfile, Rerank, InferenceProfile("embed") → ErrLLMNotConfigured
	// no ledger rows in any case
}

func TestInstallation_InferenceProfileNeedsNoTenant(t *testing.T) {
	// g.InferenceProfile(context.Background(), "embed") → ID embedID, Boundary local, Dimensions 4, MaxInputTokens 8192
}

func TestInstallation_EndpointSwappedUnderARunningProfile(t *testing.T) {
	// 1. EmbedWithProfile ok.
	// 2. fake.SetConfig(same identity, Dimensions 8) without advancing the clock → EmbedWithProfile → ErrLLMProfileMismatch;
	//    that call's row: Outcome "error", ErrorCode "profile_mismatch" (the provider was called and is charged).
	// 3. next call (cache invalidated) → ErrLLMProfileMismatch from verification, no new row.
	// 4. fresh env: fake.SetConfig(Revision "new"); clock.Advance(61s) → EmbedWithProfile and InferenceProfile("embed") → ErrLLMProfileMismatch; no row.
}

func TestInstallation_RerankOrdersAndCutsTopN(t *testing.T) {
	// Documents ["a", "ccc", "bb"], TopN 2 → Results == [{1, 0.003}, {2, 0.002}], Profile == rerankID, Usage.InputTokens > 0;
	// row: Purpose "retrieval.rerank", ModelUUID "installation:rerank", ModelID "BAAI/bge-reranker-v2-m3"
}

func TestInstallation_BudgetExceededBeforeAnyCall(t *testing.T) {
	// store := newMemBudgetStore(1, models.BudgetActionBlock) → ErrLLMBudgetExceeded; one row Outcome "budget_blocked";
	// embed calls == 1 (the verification probe only); metrics.requests["tei/budget_blocked"] == 1
}

func TestInstallation_UnavailableEndpointOpensTheBreaker(t *testing.T) {
	// verify ok; fake.Fail("/embed", 0, 503); three calls → ErrLLMProviderUnavailable, three rows ErrorCode "provider_unavailable";
	// fourth → ErrLLMProviderUnavailable, embed call count unchanged, still three rows
}

func TestInstallation_EndpointsNeverReachTheOrgCatalog(t *testing.T) {
	// with installation wired: ListUsable(ctxFor("t1")) still 2 models, none Provider "tei" or UUID prefix "installation:";
	// ctxFor("t9") (no models): ListUsable empty, Resolve and Embed(org) → ErrLLMNotConfigured;
	// catalog.CreateCredential(ctxFor("t1"), {Name "T", Provider "tei", BaseURL "http://tei:80"}) →
	//   errors.Is(err, iface.ErrLLMInvalidRequest) && errors.Is(err, models.ErrInvalidProvider)
}
```

`repository/budget_txn_test.go` (skips without `MONGO_TEST_URI`):

```go
func TestBudgetStore_InstallationKindCountsAsOrg(t *testing.T) {
	// pendingEvent("t1", 1, 50) with CredentialKind "installation", Provider "tei", ModelUUID "installation:embed", UserUUID "";
	// Reserve without budget; Finalize {Outcome ok, InputTokens 40, CredentialKind "installation"}
	// want monthly OrgInputTokens == 40, UserAccountInputTokens == 0, Requests == 1, ReservedTokens == 0;
	//      the stored row has no "userUuid" key (bson.M lookup)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/llm/services/ -run 'Installation|ImplementsTheInstallation'`
Expected: FAIL — `undefined: NewInstallationService`.

- [ ] **Step 3: Implement** `installation.go`, the gateway methods and assertions (`_ iface.LLMInferenceProfiles = (*Gateway)(nil)` and the two others), the `errorCodeOf` case and the `finalizeCall` extraction.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test -race ./internal/core/llm/services/ ./internal/core/llm/repository/`
Expected: PASS (the existing router tests included). Then with `MONGO_TEST_URI` set: `go test ./internal/core/llm/repository/ -run InstallationKind` → PASS.

- [ ] **Step 5: Commit** — `feat(llm): serve pinned embeddings and reranking to retrieval`

---

### Task 6: Module wiring, documentation and CI

**Files:**
- Modify: `backend/internal/core/llm/module.go` (struct L67-107; `Init` after `m.gateway.SetRouter(router)` L356)
- Modify: `backend/internal/core/llm/module_test.go:575-588` (`initModule` clears the two variables)
- Create: `backend/internal/core/llm/module_installation_test.go`
- Modify: `backend/internal/core/llm/AGENTS.md`, `docs/site/modules/core/llm.mdx`, `docker/AGENTS.md:141-160`, `AGENTS.md` (root, llm row of the module table)

**Interfaces:**
- Consumes: `installation.Parse`, `installation.NewSet`, `installation.Options{}` defaults (Tasks 3–4); `providers.NewInstallationHTTPClient` (Task 2); `services.NewInstallationService`, `Gateway.SetInstallation` (Task 5).
- Produces: the registered `ServiceLLMGateway` value answers the three interfaces; `Module.installation *services.InstallationService` (tests only).

`Init` addition, after the router is wired and before `deps.Services.Register(module.ServiceLLMGateway, m.gateway)`:

1. `eps, warns := installation.Parse(os.Getenv(installation.EndpointsEnv), os.Getenv, productionLike)`; each warning → `logger.Warn("llm: " + w)`;
2. each endpoint → one `logger.Info("llm: installation inference endpoint", name, kind, boundary, model, revision, profile)` (never the URL's key, never `APIKey`);
3. `set := installation.NewSet(eps, providers.NewInstallationHTTPClient(providers.TransportConfig{IdleTimeout: streamIdleTimeout}), installation.Options{})`;
4. `m.installation = services.NewInstallationService(set, budgetSvc, services.NewBreaker(3, 30*time.Second), <the router's RouterConfig accessor>, metrics.Default(), logger)`; `m.gateway.SetInstallation(m.installation)` — always, even with zero endpoints.

- [ ] **Step 1: Write the failing tests**

In `initModule`, add `t.Setenv(installation.EndpointsEnv, "")` and `t.Setenv(installation.APIKeyEnv, "")`, so a developer's shell never changes the existing WARN counts.

`module_installation_test.go` (`prodLikePlatform` implements `module.PlatformInfo` with `IsProductionLike() == true`, `IsStaging() == true`, `GetEnvironment() == "staging"`, everything else false/empty):

```go
func TestModule_InitWithoutInferenceEndpointsIsSilent(t *testing.T) {
	// initModule(t, valid key): logs has no "RETRIEVAL_INFERENCE" and no WARN;
	// m.gateway.InferenceProfile(context.Background(), "embed") → ErrLLMNotConfigured
}

func TestModule_GatewayExposesInstallationInterfaces(t *testing.T) {
	// module.GetTyped[iface.LLMInferenceProfiles], [iface.LLMPinnedEmbedder], [iface.LLMReranker]
	// on module.ServiceLLMGateway → all ok
}

func TestModule_MalformedInferenceEndpointsWarnAndBoot(t *testing.T) {
	// EndpointsEnv `[{"name":"embed",`, APIKeyEnv "sekrit-xyz": Init returns nil;
	// logs contain "level=WARN" and "RETRIEVAL_INFERENCE_ENDPOINTS"; logs do not contain "sekrit-xyz";
	// InferenceProfile(ctx, "embed") → ErrLLMNotConfigured
}

func TestModule_InstallationEndpointsReachPrivateHostsButOrgModelsCannot(t *testing.T) {
	// srv := teitest.New(t, embed fake, Dimensions 8, APIKey "inst-key-7f3a") — 127.0.0.1
	// EndpointsEnv = the roadmap embed element with url srv.URL; APIKeyEnv "inst-key-7f3a"; Platform prodLikePlatform{}
	// Init ok; logs contain the INFO line with "embed" and embedID, never "inst-key-7f3a"
	// p, _ := m.gateway.InferenceProfile(context.Background(), "embed") → Dimensions 8, no error
	// org path, same module: prov, _ := m.registry.Build(models.ProviderOllama, providers.Credential{BaseURL: srv.URL}, nil)
	//   _, err := prov.Embed(context.Background(), providers.EmbedCall{ModelID: "x", Inputs: []string{"hi"}})
	//   → errors.Is(err, providers.ErrDestinationNotPublic)   (same for models.ProviderOpenAICompatible)
	// m.registry.Build(models.ProviderTEI, providers.Credential{BaseURL: srv.URL}, nil) → errors.Is(err, providers.ErrUnknownProvider)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./internal/core/llm/ -run 'Installation|InferenceEndpoints'`
Expected: FAIL — `InferenceProfile` answers `ErrLLMNotConfigured` with the fake configured (nothing wired), and `m.installation` is undefined.

- [ ] **Step 3: Implement the `Init` wiring** as listed above.

- [ ] **Step 4: Run to verify they pass**

Run: `cd backend && go test -race ./internal/core/llm/... ./pkg/sdk/iface/... ./pkg/sdk/metrics/...`
Expected: PASS.

- [ ] **Step 5: Update the documentation**

- `backend/internal/core/llm/AGENTS.md`: Purpose — the module also serves operator-declared installation endpoints to `retrieval` (ADR-0023). «What it owns» rows for `installation/` (env parsing, profile IDs, `Set` verification cache), `providers/tei.go`, `providers/teitest/`, `services/installation.go`. «Dependencies → Environment»: `RETRIEVAL_INFERENCE_ENDPOINTS`, `RETRIEVAL_INFERENCE_API_KEY`, `RETRIEVAL_INFERENCE_*` per-endpoint keys. «Lifecycle → Init»: the four wiring steps, WARN-never-fatal, silent when unset. A new section **«Installation inference endpoints (ADR-0023)»**: the element fields and the validation table of Task 3; the profile ID definition (canonical JSON of the eight fields; name, URL and boundary excluded); verification (`/info` fields compared, the probe, `VerifyInterval` 1 min, `FailureBackoff` 5 s, `VerifyTimeout` 10 s, dimension check on every call); the call contract (caller `retrieval`, `OnBehalfOf` = context tenant, internal tenant, limits, the error table of Task 5, no fallback between profiles, breaker 3/30 s keyed `installation:<name>`); the ledger values and the **rows-without-a-user decision** from Global Constraints; isolation (`tei` not in `models.Providers`, not in `providers.Registry`, so never in `/admin/llm`, `ListUsable`, `Resolve`, `Candidates` or `HasLiveModel`; the installation client skips netguard and only ever sees env URLs; organization endpoints keep the guard, `TestModule_InstallationEndpointsReachPrivateHostsButOrgModelsCannot`). «Gateway contract»: the registered gateway also implements the three interfaces. «Providers» table: `tei` row (installation-only, hand-written `/info`, `/embed`, `/rerank`). «Operational → Metrics»: provider label `tei`. «Known limitations»: a swap to a model of the same dimension count is detected at the next re-verification (≤ 1 min); `model` and `revision` must equal TEI's `--model-id` and `--revision` exactly (`/info` reports them as `model_id` and `model_sha`; a model loaded from a local path reports the path); the usage report shows installation rows under `installation:<name>` and indexing rows under an empty user.
- `docs/site/modules/core/llm.mdx`: a section «Installation inference endpoints» (human-facing version of the above: what it is for, the variable with the roadmap example, the boundary declaration, verification, billing, that organization endpoints keep the private-address rule); a `tei` row in «Providers» marked installation-only; `tei` in the «Metrics» provider label; a sentence in «Usage» about `installation:<name>` rows and rows without a user; «Status» mentions it.
- `docker/AGENTS.md`, table «What belongs in `.env` / compose vs `/admin/modules`»: one row for `RETRIEVAL_INFERENCE_ENDPOINTS` / `RETRIEVAL_INFERENCE_API_KEY` / `RETRIEVAL_INFERENCE_*` — process; read by `llm` Init (ADR-0023 D4/D9); JSON array of named TEI endpoints with `url`, `kind`, `boundary`, `model`, `revision`, …; operator-declared only, so they may sit on the stack's internal network; invalid entries are a WARN, never a boot failure; not yet passed by the compose files nor checked by `env-validate` (the `docker-compose.retrieval.yml` overlay PR) — ✅ yes.
- Root `AGENTS.md`, llm row: append "; installation-level inference endpoints for `retrieval` (ADR-0023)".

- [ ] **Step 6: Full verification**

Run: `make ci-backend` (repo root)
Expected: exit 0 (lint, tenantscope, policycoverage, piiscan, logscope, vuln, tests, build, openapi-check — no route changed, so `openapi/enterprise.json` is unchanged).
Run: `cd backend && MONGO_TEST_URI='<replica-set URI on the dev instance>' go test ./internal/core/llm/repository/...`
Expected: PASS.

- [ ] **Step 7: Commit** — `feat(llm): wire installation inference endpoints into the gateway`
