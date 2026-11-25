package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

type stubSource struct{}

func (stubSource) Load(context.Context, string, []string) ([]iface.RetrievalLoaded, error) {
	return nil, nil
}

func (stubSource) Enumerate(context.Context, string, string, int) (iface.RetrievalPage, error) {
	return iface.RetrievalPage{}, nil
}

func (stubSource) Visible(context.Context, string, iface.RetrievalPrincipalContext, []string) (map[string]iface.RetrievalVisibility, error) {
	return nil, nil
}

type stubContributor struct{ name string }

func (c stubContributor) Name() string { return c.name }
func (c stubContributor) Principals(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func validSpec() iface.RetrievalCorpusSpec {
	return iface.RetrievalCorpusSpec{
		Name:       "contacts",
		Owner:      "crm",
		Permission: "crm.contact.read",
		Filters: []iface.RetrievalFilterField{
			{Name: "status", Type: iface.RetrievalFieldKeyword, Description: "Contact status"},
		},
		Ingestion: iface.RetrievalIngestionProfile{
			EmbedEndpoint: "embed",
			Chunking:      iface.RetrievalChunking{Strategy: "none"},
		},
		Source: stubSource{},
	}
}

func TestRegisterCorpus_NormalizesDefaults(t *testing.T) {
	s := New(Options{})
	noErr(t, s.RegisterCorpus(validSpec()))
	got, ok := s.Corpus("contacts")
	if !ok {
		t.Fatal("corpus not found")
	}
	eq(t, iface.RetrievalLocalOnly, got.Locality)
	eq(t, "word", got.Ingestion.Analyzer.Tokenizer)
	eq(t, DefaultFusion, got.Fusion)
	eq(t, 20, got.Rerank.TopK)
}

func TestRegisterCorpus_Rejects(t *testing.T) {
	filters := func(n int) []iface.RetrievalFilterField {
		out := make([]iface.RetrievalFilterField, n)
		for i := range out {
			out[i] = iface.RetrievalFilterField{Name: fmt.Sprintf("f%d", i), Type: iface.RetrievalFieldKeyword, Description: "d"}
		}
		return out
	}
	oneFilter := func(name string) []iface.RetrievalFilterField {
		return []iface.RetrievalFilterField{{Name: name, Type: iface.RetrievalFieldKeyword, Description: "d"}}
	}
	tests := []struct {
		name string
		mut  func(*iface.RetrievalCorpusSpec)
	}{
		{"uppercase name", func(sp *iface.RetrievalCorpusSpec) { sp.Name = "Contacts" }},
		{"name too long", func(sp *iface.RetrievalCorpusSpec) { sp.Name = "a" + strings.Repeat("b", 32) }},
		{"duplicate name", func(sp *iface.RetrievalCorpusSpec) { sp.Name = "taken" }},
		{"empty owner", func(sp *iface.RetrievalCorpusSpec) { sp.Owner = "" }},
		{"bad permission", func(sp *iface.RetrievalCorpusSpec) { sp.Permission = "read" }},
		{"nil source", func(sp *iface.RetrievalCorpusSpec) { sp.Source = nil }},
		{"33 filters", func(sp *iface.RetrievalCorpusSpec) { sp.Filters = filters(33) }},
		{"duplicate filter", func(sp *iface.RetrievalCorpusSpec) { sp.Filters = append(oneFilter("a"), oneFilter("a")...) }},
		{"filter type text", func(sp *iface.RetrievalCorpusSpec) { sp.Filters[0].Type = "text" }},
		{"empty description", func(sp *iface.RetrievalCorpusSpec) { sp.Filters[0].Description = "  " }},
		{"filter name _rt", func(sp *iface.RetrievalCorpusSpec) { sp.Filters = oneFilter("_rt") }},
		{"filter name _rt.tenantId", func(sp *iface.RetrievalCorpusSpec) { sp.Filters = oneFilter("_rt.tenantId") }},
		{"filter name f.status", func(sp *iface.RetrievalCorpusSpec) { sp.Filters = oneFilter("f.status") }},
		{"filter name tenantId.x", func(sp *iface.RetrievalCorpusSpec) { sp.Filters = oneFilter("tenantId.x") }},
		{"empty embed endpoint", func(sp *iface.RetrievalCorpusSpec) { sp.Ingestion.EmbedEndpoint = "" }},
		{"tokenizer code", func(sp *iface.RetrievalCorpusSpec) { sp.Ingestion.Analyzer.Tokenizer = "code" }},
		{"stemming without language", func(sp *iface.RetrievalCorpusSpec) { sp.Ingestion.Analyzer.Stemming = true }},
		{"stemming latin", func(sp *iface.RetrievalCorpusSpec) {
			sp.Ingestion.Analyzer.Stemming = true
			sp.Ingestion.Analyzer.Language = "latin"
		}},
		{"hierarchical chunking", func(sp *iface.RetrievalCorpusSpec) { sp.Ingestion.Chunking.Strategy = "hierarchical" }},
		{"fixed overlap == max", func(sp *iface.RetrievalCorpusSpec) {
			sp.Ingestion.Chunking = iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 100, Overlap: 100}
		}},
		{"none with max tokens", func(sp *iface.RetrievalCorpusSpec) { sp.Ingestion.Chunking.MaxTokens = 100 }},
		{"enrichment set", func(sp *iface.RetrievalCorpusSpec) { sp.Ingestion.Enrichment = &iface.RetrievalEnrichment{} }},
		{"locality anywhere", func(sp *iface.RetrievalCorpusSpec) { sp.Locality = "anywhere" }},
		{"dense weight 11", func(sp *iface.RetrievalCorpusSpec) { sp.Fusion.DenseWeight = 11 }},
		{"candidates 501", func(sp *iface.RetrievalCorpusSpec) { sp.Fusion.Candidates = 501 }},
		{"rerank without endpoint", func(sp *iface.RetrievalCorpusSpec) { sp.Rerank.Enabled = true }},
		{"rerank topK 201", func(sp *iface.RetrievalCorpusSpec) { sp.Rerank.TopK = 201 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{})
			taken := validSpec()
			taken.Name = "taken"
			noErr(t, s.RegisterCorpus(taken))
			sp := validSpec()
			tc.mut(&sp)
			err := s.RegisterCorpus(sp)
			isInvalid(t, err)
			errContains(t, err, "retrieval: corpus \"")
		})
	}
}

// The registry accepts exactly what chunk.Validate accepts (ruling R11): a
// larger window would pass registration and fail every item as invalid_item.
func TestRegisterCorpus_FixedChunkingFollowsTheChunkBounds(t *testing.T) {
	for _, tc := range []struct {
		maxTokens int
		ok        bool
	}{{1, true}, {4096, true}, {4097, false}, {8192, false}} {
		t.Run(fmt.Sprint(tc.maxTokens), func(t *testing.T) {
			s := New(Options{})
			sp := validSpec()
			sp.Ingestion.Chunking = iface.RetrievalChunking{Strategy: "fixed", MaxTokens: tc.maxTokens}
			err := s.RegisterCorpus(sp)
			if tc.ok {
				noErr(t, err)
				return
			}
			isInvalid(t, err)
			errContains(t, err, "4096")
		})
	}
}

func TestRegisterCorpus_ClosedAfterSeal(t *testing.T) {
	s := New(Options{})
	s.Seal()
	s.Seal()
	err := s.RegisterCorpus(validSpec())
	isInvalid(t, err)
	errContains(t, err, "registration is closed")
}

func TestRegisterPrincipalContributor_ClosedAfterSeal(t *testing.T) {
	s := New(Options{})
	s.Seal()
	err := s.RegisterPrincipalContributor(stubContributor{name: "teams"})
	isInvalid(t, err)
	errContains(t, err, "registration is closed")
}

func TestRegisterPrincipalContributor_RejectsDuplicateAndBadNames(t *testing.T) {
	s := New(Options{})
	noErr(t, s.RegisterPrincipalContributor(stubContributor{name: "teams"}))
	noErr(t, s.RegisterPrincipalContributor(stubContributor{name: "roles.x-y"}))
	for _, c := range []iface.RetrievalPrincipalContributor{nil, stubContributor{name: "teams"}, stubContributor{name: "Bad"}, stubContributor{name: ""}} {
		err := s.RegisterPrincipalContributor(c)
		isInvalid(t, err)
	}
	got := s.PrincipalContributors()
	eq(t, 2, len(got))
	eq(t, "teams", got[0].Name())
	eq(t, "roles.x-y", got[1].Name())
}

func TestFilterSchema(t *testing.T) {
	s := New(Options{})
	noErr(t, s.RegisterCorpus(validSpec()))
	first, err := s.FilterSchema("contacts")
	noErr(t, err)
	eq(t, 1, len(first))
	eq(t, "status", first[0].Name)
	first[0].Name = "mutated"
	second, err := s.FilterSchema("contacts")
	noErr(t, err)
	eq(t, "status", second[0].Name)

	c, _ := s.Corpus("contacts")
	c.Filters[0].Name = "mutated"
	third, _ := s.FilterSchema("contacts")
	eq(t, "status", third[0].Name)

	_, err = s.FilterSchema("nope")
	if !errors.Is(err, iface.ErrRetrievalUnknownCorpus) {
		t.Fatalf("got %v", err)
	}
}

func TestRegisterCorpus_StoresDeepCopy(t *testing.T) {
	s := New(Options{})
	sp := validSpec()
	noErr(t, s.RegisterCorpus(sp))
	sp.Filters[0].Name = "mutated"
	got, _ := s.Corpus("contacts")
	eq(t, "status", got.Filters[0].Name)
}

func TestCorpora_SortedByName(t *testing.T) {
	s := New(Options{})
	for _, n := range []string{"b", "a"} {
		sp := validSpec()
		sp.Name = n
		noErr(t, s.RegisterCorpus(sp))
	}
	got := s.Corpora()
	eq(t, 2, len(got))
	eq(t, "a", got[0].Name)
	eq(t, "b", got[1].Name)
}

func TestEngine_NotConfigured(t *testing.T) {
	_, err := New(Options{}).Engine()
	if !errors.Is(err, iface.ErrRetrievalNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestRegisterCorpus_IsConcurrencySafe(t *testing.T) {
	s := New(Options{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sp := validSpec()
			sp.Name = fmt.Sprintf("c%d", i)
			if err := s.RegisterCorpus(sp); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	eq(t, 16, len(s.Corpora()))
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func isInvalid(t *testing.T, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, iface.ErrRetrievalInvalidSpec) {
		t.Fatalf("want ErrRetrievalInvalidSpec, got %v", err)
	}
}

func errContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Fatalf("want error containing %q, got %v", sub, err)
	}
}

func eq[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Fatalf("want %v, got %v", want, got)
	}
}
