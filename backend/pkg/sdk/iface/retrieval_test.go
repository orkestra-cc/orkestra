package iface

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestRetrievalSentinels_AreDistinctAndWrappable(t *testing.T) {
	all := []error{
		ErrRetrievalNotConfigured, ErrRetrievalUnavailable, ErrRetrievalCorpusUnavailable,
		ErrRetrievalUnknownCorpus, ErrRetrievalInvalidSpec, ErrRetrievalInvalidFilter,
		ErrRetrievalInvalidRequest, ErrRetrievalForbidden, ErrRetrievalLocality,
		ErrRetrievalVisibilityCheck,
	}
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.Error()] {
			t.Fatalf("duplicate sentinel message %q", e.Error())
		}
		seen[e.Error()] = true
		wrapped := fmt.Errorf("ctx: %w", e)
		if !errors.Is(wrapped, e) {
			t.Fatalf("errors.Is must see through wrapping for %v", e)
		}
	}
}

func TestRetrievalFilter_JSONOmitsEmptyBranches(t *testing.T) {
	b, err := json.Marshal(RetrievalFilter{Field: "status", Op: RetrievalOpEq, Value: "open"})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"field":"status","op":"eq","value":"open"}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}
