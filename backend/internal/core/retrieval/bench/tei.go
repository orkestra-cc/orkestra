package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	// teiBatchSize equals the server's default max-client-batch-size and the
	// production adapter's batch size (llm/providers/tei.go).
	teiBatchSize = 32
	// teiMaxBody bounds every response body read.
	teiMaxBody = 64 << 20
	// teiErrDrain bounds how much of an error body is drained (never echoed).
	teiErrDrain = 4 << 10
)

// TEIClient is the benchmark's own small client for a Hugging Face Text
// Embeddings Inference server. It measures the models, not the gateway, so it
// does not import llm/providers, but its request bodies equal the production
// adapter's: the benchmark must exercise the vector space production uses.
// Query and document prefixes are the runner's job, not the client's.
type TEIClient struct {
	BaseURL string
	APIKey  string // sent as "Authorization: Bearer <key>" when non-empty
	// Normalize and Truncate mirror the declared endpoint's profile and are
	// sent on /embed (Normalize, Truncate) and /rerank (Truncate).
	Normalize bool
	Truncate  bool
	HTTP      *http.Client
}

// TEIInfo is the identity part of GET /info. A null model_sha decodes as "".
type TEIInfo struct {
	ModelID  string `json:"model_id"`
	ModelSHA string `json:"model_sha"`
}

func (c *TEIClient) do(ctx context.Context, method, path string, payload any, out any) error {
	var rd io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("tei %s: encode request: %w", path, err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return fmt.Errorf("tei %s: invalid base URL", path)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("tei %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, teiErrDrain))
		return fmt.Errorf("tei %s: status %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, teiMaxBody))
	if err != nil {
		return fmt.Errorf("tei %s: read response: %w", path, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("tei %s: malformed response", path)
	}
	return nil
}

// Info reads GET /info.
func (c *TEIClient) Info(ctx context.Context) (TEIInfo, error) {
	var info TEIInfo
	if err := c.do(ctx, http.MethodGet, "/info", nil, &info); err != nil {
		return TEIInfo{}, err
	}
	return info, nil
}

// Embed returns one vector per input, in order, sending at most 32 inputs per
// request.
func (c *TEIClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	out := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += teiBatchSize {
		batch := inputs[start:min(start+teiBatchSize, len(inputs))]
		var vectors [][]float32
		err := c.do(ctx, http.MethodPost, "/embed", map[string]any{
			"inputs": batch, "normalize": c.Normalize, "truncate": c.Truncate,
		}, &vectors)
		if err != nil {
			return nil, err
		}
		if len(vectors) != len(batch) {
			return nil, fmt.Errorf("tei /embed: %d vectors for %d inputs", len(vectors), len(batch))
		}
		out = append(out, vectors...)
	}
	return out, nil
}

// Rerank scores every text. Index is global across batches of at most 32; the
// result is ordered by Score descending, then Index ascending.
func (c *TEIClient) Rerank(ctx context.Context, query string, texts []string) ([]iface.RerankScore, error) {
	scores := make([]iface.RerankScore, 0, len(texts))
	for start := 0; start < len(texts); start += teiBatchSize {
		batch := texts[start:min(start+teiBatchSize, len(texts))]
		var items []struct {
			Index *int     `json:"index"`
			Score *float32 `json:"score"`
		}
		err := c.do(ctx, http.MethodPost, "/rerank", map[string]any{
			"query": query, "texts": batch, "truncate": c.Truncate, "raw_scores": false, "return_text": false,
		}, &items)
		if err != nil {
			return nil, err
		}
		if len(items) != len(batch) {
			return nil, fmt.Errorf("tei /rerank: %d scores for %d texts", len(items), len(batch))
		}
		seen := make([]bool, len(batch))
		for _, it := range items {
			if it.Index == nil || it.Score == nil || *it.Index < 0 || *it.Index >= len(batch) || seen[*it.Index] {
				return nil, fmt.Errorf("tei /rerank: malformed response")
			}
			seen[*it.Index] = true
			scores = append(scores, iface.RerankScore{Index: start + *it.Index, Score: *it.Score})
		}
	}
	sort.Slice(scores, func(a, b int) bool {
		if scores[a].Score != scores[b].Score {
			return scores[a].Score > scores[b].Score
		}
		return scores[a].Index < scores[b].Index
	})
	return scores, nil
}
