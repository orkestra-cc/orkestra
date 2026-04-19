package capability

import "testing"

func TestCapabilityValidate(t *testing.T) {
	if err := (Capability{}).Validate(); err == nil {
		t.Fatal("empty capability should fail validation")
	}
	if err := (Capability{ID: "x"}).Validate(); err == nil {
		t.Fatal("capability without Module should fail validation")
	}
	if err := (Capability{ID: "rag.query", Module: "rag"}).Validate(); err != nil {
		t.Fatalf("valid capability failed: %v", err)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	c := Capability{ID: "rag.query", Module: "rag", Title: "RAG Query"}
	if err := r.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.Get("rag.query")
	if !ok || got != c {
		t.Fatalf("get mismatch: %+v ok=%v", got, ok)
	}
	// Idempotent re-register with identical body is allowed.
	if err := r.Register(c); err != nil {
		t.Fatalf("idempotent re-register: %v", err)
	}
}

func TestRegistryCollisionRejected(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(Capability{ID: "rag.query", Module: "rag", Title: "A"})
	err := r.Register(Capability{ID: "rag.query", Module: "rag", Title: "B"})
	if err == nil {
		t.Fatal("colliding capability should be rejected")
	}
}

func TestRegistryListSorted(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(
		Capability{ID: "z.last", Module: "z"},
		Capability{ID: "a.first", Module: "a"},
		Capability{ID: "m.mid", Module: "m"},
	)
	list := r.List()
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	if list[0].ID != "a.first" || list[1].ID != "m.mid" || list[2].ID != "z.last" {
		t.Fatalf("not sorted: %+v", list)
	}
}
