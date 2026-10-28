package installation

import (
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const roadmapExample = `[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"abc123","pooling":"cls","normalize":true,"truncate":true},{"name":"rerank","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"BAAI/bge-reranker-v2-m3","revision":"def456","truncate":true}]`

const validRerank = `{"name":"rerank","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"BAAI/bge-reranker-v2-m3","revision":"def456","truncate":true}`

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// embedWith builds an embed element from the valid base with the given
// replacements of `"key":value` pairs (value is raw JSON; "" removes the key).
func embedWith(over map[string]string) string {
	base := []struct{ k, v string }{
		{"name", `"embed"`}, {"url", `"http://tei-embed:80"`}, {"kind", `"embed"`}, {"boundary", `"local"`},
		{"model", `"BAAI/bge-m3"`}, {"revision", `"abc123"`}, {"pooling", `"cls"`}, {"normalize", `true`}, {"truncate", `true`},
	}
	var parts []string
	seen := map[string]bool{}
	for _, kv := range base {
		seen[kv.k] = true
		v := kv.v
		if o, ok := over[kv.k]; ok {
			v = o
		}
		if v != "" {
			parts = append(parts, `"`+kv.k+`":`+v)
		}
	}
	for k, v := range over {
		if !seen[k] && v != "" {
			parts = append(parts, `"`+k+`":`+v)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func TestParse_RoadmapExample(t *testing.T) {
	eps, warns := Parse(roadmapExample, env(map[string]string{APIKeyEnv: "shared-key"}), true)
	if len(warns) != 0 || len(eps) != 2 {
		t.Fatalf("eps=%d warns=%v", len(eps), warns)
	}
	want := Endpoint{Name: "embed", URL: "http://tei-embed:80", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal,
		Model: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Normalize: true, Truncate: true, APIKey: "shared-key"}
	if eps[0] != want {
		t.Fatalf("eps[0] = %+v", eps[0])
	}
	if eps[0].Profile().ID != embedID || eps[1].Profile().ID != rerankID {
		t.Fatalf("ids: %s %s", eps[0].Profile().ID, eps[1].Profile().ID)
	}
	if eps[0].LedgerModelUUID() != "installation:embed" {
		t.Fatal(eps[0].LedgerModelUUID())
	}
	p := eps[0].Profile()
	if p.Endpoint != "embed" || p.Boundary != iface.InferenceBoundaryLocal || p.Dimensions != 0 || p.MaxInputTokens != 0 {
		t.Fatalf("profile = %+v", p)
	}
}

func TestParse_EmptyIsSilent(t *testing.T) {
	for _, raw := range []string{"", "  \n"} {
		eps, warns := Parse(raw, env(nil), true)
		if eps != nil || warns != nil {
			t.Fatalf("%q: %v %v", raw, eps, warns)
		}
	}
}

func TestParse_MalformedJSONDropsEverything(t *testing.T) {
	for _, raw := range []string{`[{"name":`, `{"name":"x"}`, `[1]`, `["a"]`} {
		eps, warns := Parse(raw, env(nil), true)
		if len(eps) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "malformed") {
			t.Fatalf("%q: %v %v", raw, eps, warns)
		}
	}
}

func TestParse_InvalidFieldDropsOnlyThatEndpoint(t *testing.T) {
	long := `"` + strings.Repeat("a", 257) + `"`
	rerankWith := func(extra string) string {
		return `{"name":"x","url":"http://h:80","kind":"rerank","boundary":"local","model":"m","revision":"r"` + extra + `}`
	}
	cases := []struct{ field, bad string }{
		{"name", embedWith(map[string]string{"name": `"Embed!"`})},
		{"url", embedWith(map[string]string{"url": `"ftp://x"`})},
		{"url", embedWith(map[string]string{"url": `"http://u:p@h"`})},
		{"url", embedWith(map[string]string{"url": `"http://h/?q=1"`})},
		{"url", embedWith(map[string]string{"url": `"http://h/#f"`})},
		{"url", embedWith(map[string]string{"url": `"http://"`})},
		{"kind", embedWith(map[string]string{"kind": `"classify"`})},
		{"boundary", embedWith(map[string]string{"boundary": ""})},
		{"model", embedWith(map[string]string{"model": `""`})},
		{"revision", embedWith(map[string]string{"revision": ""})},
		{"pooling", embedWith(map[string]string{"pooling": ""})},
		{"pooling", embedWith(map[string]string{"pooling": `"splade"`})},
		{"pooling", rerankWith(`,"pooling":"cls"`)},
		{"normalize", rerankWith(`,"normalize":true`)},
		{"queryPrefix", rerankWith(`,"queryPrefix":"q: "`)},
		{"queryPrefix", embedWith(map[string]string{"queryPrefix": long})},
		{"documentPrefix", embedWith(map[string]string{"documentPrefix": long})},
		{"truncate", `{"name":"x","url":"http://h","kind":"chat","boundary":"local","model":"m","revision":"r","truncate":true}`},
		{"normalise", embedWith(map[string]string{"normalise": `true`})},
		{"apiKeyEnv", embedWith(map[string]string{"apiKeyEnv": `"HOME"`})},
		{"apiKeyEnv", embedWith(map[string]string{"apiKeyEnv": `"RETRIEVAL_INFERENCE_KEY_EMBED"`})},
		{"normalize", embedWith(map[string]string{"normalize": `"yes"`})},
	}
	for _, c := range cases {
		eps, warns := Parse("["+c.bad+","+validRerank+"]", env(nil), true)
		if len(eps) != 1 || eps[0].Name != "rerank" {
			t.Errorf("%s: eps = %+v", c.field, eps)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "[0]: "+c.field+": ") || !strings.HasSuffix(warns[0], "; endpoint ignored") {
			t.Errorf("%s: warns = %v", c.field, warns)
		}
	}
}

func TestParse_HostedHTTPRefusedOnlyWhenProductionLike(t *testing.T) {
	hostedHTTP := embedWith(map[string]string{"boundary": `"hosted"`})
	eps, warns := Parse("["+hostedHTTP+"]", env(nil), true)
	if len(eps) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "url") {
		t.Fatalf("prod: %v %v", eps, warns)
	}
	if eps, warns = Parse("["+hostedHTTP+"]", env(nil), false); len(eps) != 1 || len(warns) != 0 {
		t.Fatalf("dev: %v %v", eps, warns)
	}
	if eps, _ = Parse("["+embedWith(nil)+"]", env(nil), true); len(eps) != 1 {
		t.Fatal("local http must pass")
	}
	hostedHTTPS := embedWith(map[string]string{"boundary": `"hosted"`, "url": `"https://api.example.com/v1/"`})
	eps, _ = Parse("["+hostedHTTPS+"]", env(nil), true)
	if len(eps) != 1 || eps[0].URL != "https://api.example.com/v1" {
		t.Fatalf("hosted https: %+v", eps)
	}
}

func TestParse_DuplicateNameDropsEveryHolder(t *testing.T) {
	other := embedWith(map[string]string{"model": `"other/model"`})
	eps, warns := Parse("["+embedWith(nil)+","+other+","+validRerank+"]", env(nil), true)
	if len(eps) != 1 || eps[0].Name != "rerank" {
		t.Fatalf("eps = %+v", eps)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], `duplicate name "embed"`) {
		t.Fatalf("warns = %v", warns)
	}
}

func TestParse_DuplicateProfileDropsEveryHolder(t *testing.T) {
	a := embedWith(map[string]string{"name": `"embed-a"`})
	b := embedWith(map[string]string{"name": `"embed-b"`, "url": `"http://other:80"`})
	eps, warns := Parse("["+a+","+b+"]", env(nil), true)
	if len(eps) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "duplicate profile") {
		t.Fatalf("%v %v", eps, warns)
	}
}

func TestParse_PerEndpointKey(t *testing.T) {
	rerank := strings.Replace(validRerank, `"truncate":true`, `"truncate":true,"apiKeyEnv":"RETRIEVAL_INFERENCE_API_KEY_RERANK"`, 1)
	eps, warns := Parse("["+embedWith(nil)+","+rerank+"]", env(map[string]string{
		APIKeyEnv: "k1", "RETRIEVAL_INFERENCE_API_KEY_RERANK": "k2",
	}), true)
	if len(warns) != 0 || len(eps) != 2 || eps[0].APIKey != "k1" || eps[1].APIKey != "k2" {
		t.Fatalf("%+v %v", eps, warns)
	}
}

func TestParse_WarningsNeverCarryTheKey(t *testing.T) {
	e := env(map[string]string{APIKeyEnv: "sekrit-123"})
	bads := []string{
		embedWith(map[string]string{"name": `"Embed!"`}),
		embedWith(map[string]string{"url": `"http://u:sekrit-123@h"`}),
		embedWith(map[string]string{"apiKeyEnv": `"sekrit-123"`}),
		embedWith(map[string]string{"apiKeyEnv": `"RETRIEVAL_INFERENCE_UNSET"`}),
		embedWith(map[string]string{"kind": `"classify"`}),
		embedWith(map[string]string{"extra": `"sekrit-123"`}),
	}
	_, warns := Parse("["+strings.Join(bads, ",")+","+roadmapExample[1:], e, true)
	if len(warns) < len(bads) {
		t.Fatalf("warns = %v", warns)
	}
	if strings.Contains(strings.Join(warns, "\n"), "sekrit-123") {
		t.Fatalf("key leaked: %v", warns)
	}
}

func TestParse_ChatIsParsed(t *testing.T) {
	raw := `[{"name":"chat","url":"http://llm:8000","kind":"chat","boundary":"local","model":"Qwen/Qwen3","revision":"r1"}]`
	eps, warns := Parse(raw, env(nil), true)
	if len(warns) != 0 || len(eps) != 1 || eps[0].Kind != iface.InferenceKindChat {
		t.Fatalf("%+v %v", eps, warns)
	}
}

func TestParse_JSONNullIsMalformedButEmptyArrayIsSilent(t *testing.T) {
	eps, warns := Parse("null", env(nil), true)
	if len(eps) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "malformed") {
		t.Fatalf("null: %v %v", eps, warns)
	}
	eps, warns = Parse("[]", env(nil), true)
	if len(eps) != 0 || len(warns) != 0 {
		t.Fatalf("[]: %v %v", eps, warns)
	}
}
