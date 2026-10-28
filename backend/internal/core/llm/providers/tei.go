package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TEIDefaultBatchSize is the batch size used when an option leaves it unset;
// it equals the server's default max-client-batch-size.
const TEIDefaultBatchSize = 32

const (
	teiMaxInfoBytes  = 1 << 20
	teiErrDrainBytes = 4 << 10
)

// TEI talks to a Hugging Face Text Embeddings Inference server: GET /info,
// POST /embed and POST /rerank. It is an installation-level adapter (ADR-0023):
// not registered in RegisterAll, never selectable for an organization
// credential. Build its client with NewInstallationHTTPClient.
type TEI struct {
	base   string
	secret string
	client *http.Client
}

// NewTEI builds the adapter. A nil client is refused rather than replaced by
// http.DefaultClient.
func NewTEI(baseURL, apiKey string, client *http.Client) (*TEI, error) {
	if client == nil {
		return nil, errors.New("llm providers: tei requires an http client")
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("%w: tei requires a base URL", iface.ErrLLMInvalidRequest)
	}
	return &TEI{base: strings.TrimRight(baseURL, "/"), secret: apiKey, client: client}, nil
}

// TEIInfo is the identity part of GET /info.
type TEIInfo struct {
	ModelID            string       `json:"model_id"`
	ModelSHA           *string      `json:"model_sha"`
	ModelType          TEIModelType `json:"model_type"`
	MaxInputLength     int          `json:"max_input_length"`
	MaxClientBatchSize int          `json:"max_client_batch_size"`
}

// TEIModelType is the one-of model_type object; exactly one member is set.
type TEIModelType struct {
	Embedding  *TEIEmbeddingType `json:"embedding,omitempty"`
	Reranker   json.RawMessage   `json:"reranker,omitempty"` // present = len > 0
	Classifier json.RawMessage   `json:"classifier,omitempty"`
}

type TEIEmbeddingType struct {
	Pooling string `json:"pooling"`
}

type TEIEmbedOptions struct {
	Normalize, Truncate bool
	BatchSize           int // <= 0 -> TEIDefaultBatchSize
}

type TEIRerankOptions struct {
	Truncate  bool
	BatchSize int // <= 0 -> TEIDefaultBatchSize
}

func (t *TEI) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", iface.ErrLLMInvalidRequest)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.secret != "" {
		req.Header.Set("Authorization", "Bearer "+t.secret)
	}
	return req, nil
}

// classifyTEI reduces an HTTP error status to a sentinel; the body is never
// surfaced.
func classifyTEI(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %w: tei status %d", ErrUnauthorized, iface.ErrLLMProviderUnavailable, status)
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: tei status %d", iface.ErrLLMInvalidRequest, status)
	default:
		return fmt.Errorf("%w: tei status %d", iface.ErrLLMProviderUnavailable, status)
	}
}

func (t *TEI) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	if resp.StatusCode >= 400 || resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, teiErrDrainBytes))
		return nil, classifyTEI(resp.StatusCode)
	}
	return resp, nil
}

func (t *TEI) post(ctx context.Context, path string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: request is not serialisable", iface.ErrLLMInvalidRequest)
	}
	req, err := t.newRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return t.send(ctx, req)
}

// Info reads GET /info.
func (t *TEI) Info(ctx context.Context) (*TEIInfo, error) {
	req, err := t.newRequest(ctx, http.MethodGet, "/info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.send(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ReadAllBounded(resp.Body, teiMaxInfoBytes)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	var info TEIInfo
	if err := json.Unmarshal(body, &info); err != nil || info.ModelID == "" {
		return nil, malformed("info response")
	}
	return &info, nil
}

func batchSize(n int) int {
	if n <= 0 {
		return TEIDefaultBatchSize
	}
	return n
}

// computeTokens returns the positive x-compute-tokens header, else fallback.
func computeTokens(h http.Header, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("x-compute-tokens"))); err == nil && n > 0 {
		return n
	}
	return fallback
}

func ceilThirds(n int) int { return (n + 2) / 3 }

// Embed returns one vector per input, in order. A failure after completed
// batches returns a non-nil outcome whose Usage covers those batches (and no
// vectors), like Provider.Embed.
func (t *TEI) Embed(ctx context.Context, inputs []string, o TEIEmbedOptions) (*EmbedOutcome, error) {
	size := batchSize(o.BatchSize)
	out := &EmbedOutcome{Vectors: make([][]float32, 0, len(inputs))}
	completed := false
	fail := func(err error) (*EmbedOutcome, error) {
		if completed {
			return &EmbedOutcome{Usage: out.Usage}, err
		}
		return nil, err
	}
	dims := 0
	for start := 0; start < len(inputs); start += size {
		batch := inputs[start:min(start+size, len(inputs))]
		resp, err := t.post(WithResponseCap(ctx, MaxEmbedResponseBytes), "/embed",
			map[string]any{"inputs": batch, "normalize": o.Normalize, "truncate": o.Truncate})
		if err != nil {
			return fail(err)
		}
		body, err := ReadAllBounded(resp.Body, MaxEmbedResponseBytes)
		resp.Body.Close()
		if err != nil {
			return fail(transportErr(ctx, err))
		}
		var vectors [][]float32
		if err := json.Unmarshal(body, &vectors); err != nil || len(vectors) != len(batch) {
			return fail(malformed("embeddings response"))
		}
		for _, v := range vectors {
			if len(v) == 0 {
				return fail(malformed("embeddings response"))
			}
			if dims != 0 && len(v) != dims {
				// The model behind the endpoint changed mid-call: a profile
				// change, not an outage (no breaker verdict, profile re-verified).
				return fail(fmt.Errorf("%w: embedding dimensions changed within one call", iface.ErrLLMProfileMismatch))
			}
			dims = len(v)
		}
		n := 0
		for _, in := range batch {
			n += len(in)
		}
		out.Vectors = append(out.Vectors, vectors...)
		out.Usage.InputTokens += computeTokens(resp.Header, ceilThirds(n))
		completed = true
	}
	return out, nil
}

// Rerank scores every text. Index is global; the result is ordered by Score
// descending then Index ascending. The usage covers completed batches even
// when an error is returned.
func (t *TEI) Rerank(ctx context.Context, query string, texts []string, o TEIRerankOptions) ([]iface.RerankScore, iface.LLMUsage, error) {
	size := batchSize(o.BatchSize)
	var usage iface.LLMUsage
	scores := make([]iface.RerankScore, 0, len(texts))
	for start := 0; start < len(texts); start += size {
		batch := texts[start:min(start+size, len(texts))]
		resp, err := t.post(ctx, "/rerank", map[string]any{
			"query": query, "texts": batch, "truncate": o.Truncate, "raw_scores": false, "return_text": false,
		})
		if err != nil {
			return nil, usage, err
		}
		body, err := ReadAllBounded(resp.Body, MaxEmbedResponseBytes)
		resp.Body.Close()
		if err != nil {
			return nil, usage, transportErr(ctx, err)
		}
		var items []struct {
			Index *int     `json:"index"`
			Score *float32 `json:"score"`
		}
		if err := json.Unmarshal(body, &items); err != nil || len(items) != len(batch) {
			return nil, usage, malformed("rerank response")
		}
		seen := make([]bool, len(batch))
		for _, it := range items {
			if it.Index == nil || it.Score == nil || *it.Index < 0 || *it.Index >= len(batch) || seen[*it.Index] {
				return nil, usage, malformed("rerank response")
			}
			seen[*it.Index] = true
			scores = append(scores, iface.RerankScore{Index: start + *it.Index, Score: *it.Score})
		}
		n := len(query) * len(batch)
		for _, tx := range batch {
			n += len(tx)
		}
		usage.InputTokens += computeTokens(resp.Header, ceilThirds(n))
	}
	sort.Slice(scores, func(a, b int) bool {
		if scores[a].Score != scores[b].Score {
			return scores[a].Score > scores[b].Score
		}
		return scores[a].Index < scores[b].Index
	})
	return scores, usage, nil
}
