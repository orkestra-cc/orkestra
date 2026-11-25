package providers

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Final review I1: the contract admits 256 embedding inputs, and the
// largest common models return 3072 dimensions. Through the module's
// default client (NewHTTPClient with no MaxResponseBytes, i.e. the 8 MiB
// non-stream cap) every embedding adapter must accept a batch at the
// contract maximum, while every other non-stream call stays capped at
// 8 MiB.
const (
	contractMaxInputs = 256
	largeDims         = 3072
)

// embedValue is a deterministic, realistically long float (vendors print
// about ten significant digits).
func embedValue(i, k int) float32 { return float32(-0.0123456789 + float64(i*largeDims+k)*1e-9) }

func floatVectorJSON(i int) string {
	var b strings.Builder
	b.WriteByte('[')
	for k := 0; k < largeDims; k++ {
		if k > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(embedValue(i, k)), 'g', 10, 64))
	}
	b.WriteByte(']')
	return b.String()
}

func base64Vector(i int) string {
	raw := make([]byte, 4*largeDims)
	for k := 0; k < largeDims; k++ {
		binary.LittleEndian.PutUint32(raw[4*k:], math.Float32bits(embedValue(i, k)))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func inputs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "chunk " + strconv.Itoa(i)
	}
	return out
}

// defaultClient mirrors the module's clientFor: no explicit response cap.
func defaultClient() *http.Client { return NewHTTPClient(TransportConfig{ProductionLike: false}) }

func checkVectors(t *testing.T, out *EmbedOutcome, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("embed at the contract maximum: %v", err)
	}
	if len(out.Vectors) != contractMaxInputs {
		t.Fatalf("vectors = %d", len(out.Vectors))
	}
	for _, i := range []int{0, 137, contractMaxInputs - 1} {
		v := out.Vectors[i]
		if len(v) != largeDims || v[0] != embedValue(i, 0) || v[largeDims-1] != embedValue(i, largeDims-1) {
			t.Fatalf("vector %d: len %d, first %v want %v", i, len(v), v[0], embedValue(i, 0))
		}
	}
}

func TestOpenAI_EmbedAtTheContractMaximumRequestsBase64(t *testing.T) {
	var format any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		format = body["encoding_format"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","model":"text-embedding-3-large","usage":{"prompt_tokens":512,"total_tokens":512},"data":[`)
		for i := 0; i < contractMaxInputs; i++ {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			vec := floatVectorJSON(i) // what OpenAI answers without encoding_format
			if format == "base64" {
				vec = `"` + base64Vector(i) + `"`
			}
			_, _ = io.WriteString(w, `{"object":"embedding","index":`+strconv.Itoa(i)+`,"embedding":`+vec+`}`)
		}
		_, _ = io.WriteString(w, `]}`)
	}))
	t.Cleanup(srv.Close)
	p, err := NewOpenAI(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "sk-test", BaseURL: srv.URL + "/v1"}, defaultClient())
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Embed(context.Background(), EmbedCall{ModelID: "text-embedding-3-large", Inputs: inputs(contractMaxInputs)})
	checkVectors(t, out, err)
	if format != "base64" {
		t.Errorf("encoding_format = %v, want base64", format)
	}
	if out.Usage.InputTokens != 512 {
		t.Errorf("usage = %+v", out.Usage)
	}
}

func TestOpenAI_EmbedRejectsMalformedBase64(t *testing.T) {
	for _, vec := range []string{`"not base64!"`, `"AAA="`, `""`, `{}`} {
		p, _ := newOAI(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":`+vec+`}],"usage":{"prompt_tokens":1}}`)
		}, iface.LLMCredentialKindOrg)
		if _, err := p.Embed(context.Background(), EmbedCall{ModelID: "e", Inputs: []string{"a"}}); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
			t.Errorf("embedding %s: err = %v, want malformed", vec, err)
		}
	}
}

func TestOpenAICompatible_EmbedAtTheContractMaximum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","usage":{"prompt_tokens":512},"data":[`)
		for i := 0; i < contractMaxInputs; i++ {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = io.WriteString(w, `{"index":`+strconv.Itoa(i)+`,"embedding":`+floatVectorJSON(i)+`}`)
		}
		_, _ = io.WriteString(w, `]}`)
	}))
	t.Cleanup(srv.Close)
	p, err := NewOpenAICompatible(Credential{BaseURL: srv.URL + "/v1"}, defaultClient())
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Embed(context.Background(), EmbedCall{ModelID: "bge", Inputs: inputs(contractMaxInputs)})
	checkVectors(t, out, err)
}

func TestOllama_EmbedAtTheContractMaximum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"prompt_eval_count":512,"embeddings":[`)
		for i := 0; i < contractMaxInputs; i++ {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = io.WriteString(w, floatVectorJSON(i))
		}
		_, _ = io.WriteString(w, `]}`)
	}))
	t.Cleanup(srv.Close)
	p, err := NewOllama(Credential{BaseURL: srv.URL}, defaultClient())
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Embed(context.Background(), EmbedCall{ModelID: "nomic", Inputs: inputs(contractMaxInputs)})
	checkVectors(t, out, err)
}

func TestGemini_EmbedAtTheContractMaximum(t *testing.T) {
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Requests []json.RawMessage `json:"requests"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embeddings":[`)
		for j := range body.Requests {
			if j > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = io.WriteString(w, `{"values":`+floatVectorJSON(next)+`}`)
			next++
		}
		_, _ = io.WriteString(w, `]}`)
	}))
	t.Cleanup(srv.Close)
	p, err := NewGemini(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "AIza-test", BaseURL: srv.URL}, defaultClient())
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Embed(context.Background(), EmbedCall{ModelID: "gemini-embedding-001", Inputs: inputs(contractMaxInputs)})
	checkVectors(t, out, err)
}

// The larger cap is request-side and embedding-only: a non-embedding call
// of the same adapter through the same client is still cut at 8 MiB.
func TestOpenAI_NonEmbedCallStaysCappedAt8MiB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.(http.Flusher).Flush() // chunked: the body cap, not the Content-Length shortcut
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"`+strings.Repeat("x", 9<<20)+`","object":"model"}]}`)
	}))
	t.Cleanup(srv.Close)
	p, err := NewOpenAI(Credential{Kind: iface.LLMCredentialKindOrg, Secret: "sk-test", BaseURL: srv.URL + "/v1"}, defaultClient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ListModels(context.Background()); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("9 MiB /models err = %v, want the 8 MiB cap (provider unavailable)", err)
	}
}
