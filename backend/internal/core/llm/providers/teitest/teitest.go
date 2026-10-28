// Package teitest is an in-process fake of a Text Embeddings Inference
// server (/info, /embed, /rerank) with deterministic outputs. It is imported
// by tests only.
package teitest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Config selects what the fake serves.
type Config struct {
	ModelID            string        // /info model_id
	Revision           string        // /info model_sha; "" -> JSON null
	Kind               string        // "embed" (default) | "rerank"
	Pooling            string        // embed: model_type.embedding.pooling; default "cls"
	Dimensions         int           // embed vector length; default 4
	MaxInputLength     int           // default 8192
	MaxClientBatchSize int           // default 32; a larger batch answers 413
	APIKey             string        // set: every route needs "Authorization: Bearer <APIKey>", else 401
	OmitComputeTokens  bool          // do not send x-compute-tokens
	InfoDelay          time.Duration // sleep before answering /info
}

type EmbedCall struct {
	Inputs              []string
	Normalize, Truncate bool
}

type RerankCall struct {
	Query    string
	Texts    []string
	Truncate bool
}

type failRule struct{ after, status int }

// Server is the fake; URL is its base URL.
type Server struct {
	URL string

	mu      sync.Mutex
	cfg     Config
	fails   map[string]failRule
	seen    map[string]int
	info    int
	embeds  []EmbedCall
	reranks []RerankCall
	auths   []string

	dimsAfter, dimsTo int // embed calls beyond dimsAfter serve dimsTo (0 = off)
}

func normalize(cfg Config) Config {
	if cfg.Kind == "" {
		cfg.Kind = "embed"
	}
	if cfg.Pooling == "" {
		cfg.Pooling = "cls"
	}
	if cfg.Dimensions <= 0 {
		cfg.Dimensions = 4
	}
	if cfg.MaxInputLength <= 0 {
		cfg.MaxInputLength = 8192
	}
	if cfg.MaxClientBatchSize <= 0 {
		cfg.MaxClientBatchSize = 32
	}
	return cfg
}

// New starts the fake; it is closed on t.Cleanup.
func New(t testing.TB, cfg Config) *Server {
	s := &Server{cfg: normalize(cfg), fails: map[string]failRule{}, seen: map[string]int{}}
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(ts.Close)
	s.URL = ts.URL
	return s
}

// SetConfig makes the endpoint serve another model.
func (s *Server) SetConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = normalize(cfg)
}

// Fail makes requests on path beyond the first `after` answer status with an
// error body; status 0 clears the rule.
func (s *Server) Fail(path string, after, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == 0 {
		delete(s.fails, path)
		return
	}
	s.fails[path] = failRule{after: after, status: status}
	s.seen[path] = 0
}

// ChangeDimensionsAfter makes /embed requests beyond the first `after` answer
// vectors of `dims` entries (a model swapped under a running call); dims 0
// clears it.
func (s *Server) ChangeDimensionsAfter(after, dims int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dimsAfter, s.dimsTo = after, dims
}

func (s *Server) InfoCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

func (s *Server) EmbedCalls() []EmbedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]EmbedCall(nil), s.embeds...)
}

func (s *Server) RerankCalls() []RerankCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RerankCall(nil), s.reranks...)
}

// AuthHeaders returns the Authorization header of every request, in order.
func (s *Server) AuthHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auths...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.auths = append(s.auths, r.Header.Get("Authorization"))
	if r.URL.Path == "/info" {
		s.info++
	}
	rule, failing := s.fails[r.URL.Path]
	if failing {
		s.seen[r.URL.Path]++
		failing = s.seen[r.URL.Path] > rule.after
	}
	s.mu.Unlock()

	if cfg.APIKey != "" && r.Header.Get("Authorization") != "Bearer "+cfg.APIKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized", "error_type": "Unauthorized"})
		return
	}
	if r.URL.Path == "/info" && cfg.InfoDelay > 0 {
		time.Sleep(cfg.InfoDelay)
	}
	if failing {
		writeJSON(w, rule.status, map[string]string{"error": "injected", "error_type": "Backend"})
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/info":
		s.serveInfo(w, cfg)
	case r.Method == http.MethodPost && r.URL.Path == "/embed":
		s.embed(w, r, cfg)
	case r.Method == http.MethodPost && r.URL.Path == "/rerank":
		s.rerank(w, r, cfg)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveInfo(w http.ResponseWriter, cfg Config) {
	var sha *string
	if cfg.Revision != "" {
		sha = &cfg.Revision
	}
	var mt any
	if cfg.Kind == "rerank" {
		mt = map[string]any{"reranker": map[string]any{"id2label": map[string]string{"0": "LABEL_0"}}}
	} else {
		mt = map[string]any{"embedding": map[string]string{"pooling": cfg.Pooling}}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model_id": cfg.ModelID, "model_sha": sha, "model_type": mt,
		"max_input_length": cfg.MaxInputLength, "max_client_batch_size": cfg.MaxClientBatchSize,
	})
}

func (s *Server) embed(w http.ResponseWriter, r *http.Request, cfg Config) {
	var in struct {
		Inputs    []string `json:"inputs"`
		Normalize bool     `json:"normalize"`
		Truncate  bool     `json:"truncate"`
	}
	if cfg.Kind == "rerank" || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request", "error_type": "Validation"})
		return
	}
	if len(in.Inputs) > cfg.MaxClientBatchSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "batch too large", "error_type": "Validation"})
		return
	}
	s.mu.Lock()
	if s.dimsTo > 0 && len(s.embeds) >= s.dimsAfter {
		cfg.Dimensions = s.dimsTo
	}
	s.embeds = append(s.embeds, EmbedCall{Inputs: in.Inputs, Normalize: in.Normalize, Truncate: in.Truncate})
	s.mu.Unlock()
	out := make([][]float32, len(in.Inputs))
	tokens := 0
	for i, text := range in.Inputs {
		v := make([]float32, cfg.Dimensions)
		for j := range v {
			v[j] = 0.5
		}
		v[0] = float32(len(text))
		out[i] = v
		tokens += len(text)
	}
	if !cfg.OmitComputeTokens {
		w.Header().Set("x-compute-tokens", strconv.Itoa(tokens))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) rerank(w http.ResponseWriter, r *http.Request, cfg Config) {
	var in struct {
		Query    string   `json:"query"`
		Texts    []string `json:"texts"`
		Truncate bool     `json:"truncate"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request", "error_type": "Validation"})
		return
	}
	if len(in.Texts) > cfg.MaxClientBatchSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "batch too large", "error_type": "Validation"})
		return
	}
	s.mu.Lock()
	s.reranks = append(s.reranks, RerankCall{Query: in.Query, Texts: in.Texts, Truncate: in.Truncate})
	s.mu.Unlock()
	type item struct {
		Index int     `json:"index"`
		Score float32 `json:"score"`
	}
	items := make([]item, len(in.Texts))
	tokens := len(in.Query) * len(in.Texts)
	for i, text := range in.Texts {
		items[i] = item{Index: i, Score: float32(len(text)) / 1000}
		tokens += len(text)
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].Score > items[b].Score })
	if !cfg.OmitComputeTokens {
		w.Header().Set("x-compute-tokens", strconv.Itoa(tokens))
	}
	writeJSON(w, http.StatusOK, items)
}
