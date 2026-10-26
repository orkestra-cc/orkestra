package engine

import (
	"testing"

	"github.com/google/uuid"
)

func TestPointID_Golden(t *testing.T) {
	// UUIDv5, namespace 21fc1e39-ea81-5111-b6fd-a430a1211388, name "t1\x00contacts\x00s1\x000"
	cases := []struct {
		tenant, corpus, source string
		ordinal                int
		want                   string
	}{
		{"t1", "contacts", "s1", 0, "d66fd356-1bfc-517f-879d-d8d3f6b78fbb"},
		{"t1", "contacts", "s1", 1, "6fa2aea1-c234-5176-903f-a29ded43d276"},
		{"t2", "contacts", "s1", 0, "35ebe444-6537-506f-93cc-c6e4a4131fae"},
	}
	for _, c := range cases {
		got := PointID(c.tenant, c.corpus, c.source, c.ordinal)
		if got != c.want {
			t.Errorf("PointID(%q,%q,%q,%d) = %s, want %s", c.tenant, c.corpus, c.source, c.ordinal, got, c.want)
		}
		if v := uuid.MustParse(got).Version(); v != 5 {
			t.Errorf("version = %d, want 5", v)
		}
	}
}

func TestPointNamespace_Frozen(t *testing.T) {
	want := uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://orkestra.cc/retrieval/point-id/v1"))
	if pointNamespace != want || pointNamespace.String() != "21fc1e39-ea81-5111-b6fd-a430a1211388" {
		t.Fatalf("pointNamespace = %s", pointNamespace)
	}
}

func TestCollectionName(t *testing.T) {
	if got := CollectionName("orkestra", "contacts", 1); got != "orkestra_contacts_g1" {
		t.Fatal(got)
	}
}

func TestSelectorIsZero(t *testing.T) {
	if !(Selector{}).IsZero() {
		t.Error("empty selector must be zero")
	}
	if (Selector{Subject: "user:u"}).IsZero() {
		t.Error("subject selector must not be zero")
	}
	if !(Selector{SourceIDs: []string{}}).IsZero() {
		t.Error("empty SourceIDs must be zero")
	}
	if (Selector{TenantID: "t"}).IsZero() || (Selector{SourceIDs: []string{"a"}}).IsZero() {
		t.Error("set fields must not be zero")
	}
}
