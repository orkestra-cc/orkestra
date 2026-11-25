package installation

import (
	"regexp"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	embedID  = "ip_089b7e60b3d89b3c948d5b17d9e42d5008999b8f173f6da98d433d6db8027323"
	rerankID = "ip_f24b9aabce34b8f83c405595db0b73f005db166274d15b058bfcd2fc424d9fb6"
)

func embedProfile() iface.InferenceProfile {
	return iface.InferenceProfile{Kind: iface.InferenceKindEmbed, ModelID: "BAAI/bge-m3", Revision: "abc123", Pooling: "cls", Normalize: true, Truncate: true}
}

func TestProfileID_Golden(t *testing.T) {
	rerank := iface.InferenceProfile{Kind: iface.InferenceKindRerank, ModelID: "BAAI/bge-reranker-v2-m3", Revision: "def456", Truncate: true}
	if got := ProfileID(embedProfile()); got != embedID {
		t.Fatalf("embed = %s", got)
	}
	if got := ProfileID(rerank); got != rerankID {
		t.Fatalf("rerank = %s", got)
	}
}

func TestProfileID_IgnoresNameURLBoundaryAndMeasuredFields(t *testing.T) {
	p := embedProfile()
	p.Endpoint = "other"
	p.Boundary = iface.InferenceBoundaryHosted
	p.Dimensions = 1024
	p.MaxInputTokens = 8192
	p.ID = "x"
	if got := ProfileID(p); got != embedID {
		t.Fatalf("got %s", got)
	}
}

func TestProfileID_EveryIdentityFieldChangesIt(t *testing.T) {
	re := regexp.MustCompile(`^ip_[0-9a-f]{64}$`)
	muts := map[string]func(*iface.InferenceProfile){
		"Kind":           func(p *iface.InferenceProfile) { p.Kind = iface.InferenceKindRerank },
		"ModelID":        func(p *iface.InferenceProfile) { p.ModelID = "other" },
		"Revision":       func(p *iface.InferenceProfile) { p.Revision = "other" },
		"Pooling":        func(p *iface.InferenceProfile) { p.Pooling = "mean" },
		"Normalize":      func(p *iface.InferenceProfile) { p.Normalize = false },
		"Truncate":       func(p *iface.InferenceProfile) { p.Truncate = false },
		"QueryPrefix":    func(p *iface.InferenceProfile) { p.QueryPrefix = "query: " },
		"DocumentPrefix": func(p *iface.InferenceProfile) { p.DocumentPrefix = "passage: " },
	}
	for name, mut := range muts {
		p := embedProfile()
		mut(&p)
		got := ProfileID(p)
		if got == embedID {
			t.Errorf("%s did not change the ID", name)
		}
		if !re.MatchString(got) {
			t.Errorf("%s: bad ID shape %q", name, got)
		}
	}
}
