package jobs

import (
	"testing"
	"time"
)

func TestCatalogRegister(t *testing.T) {
	c := NewCatalog()
	b, a := validDef(), validDef()
	b.Name, a.Name = "b_job", "a_job"
	if err := c.Register("mod", []Definition{b, a}); err != nil {
		t.Fatal(err)
	}
	l := c.List()
	if len(l) != 2 || l[0].Key != "mod.a_job" || l[1].Key != "mod.b_job" || l[0].Module != "mod" {
		t.Fatalf("list = %+v", l)
	}
	if l[0].Def.Timeout != 10*time.Minute {
		t.Error("def not normalized")
	}
	if e, ok := c.Get("mod.b_job"); !ok || e.Def.Name != "b_job" {
		t.Error("Get failed")
	}
	if _, ok := c.Get("mod.zzz"); ok {
		t.Error("Get found missing")
	}

	// Atomic: one invalid def registers none.
	bad := validDef()
	bad.Title = ""
	ok := validDef()
	ok.Name = "fine"
	if err := c.Register("other", []Definition{ok, bad}); err == nil {
		t.Fatal("expected error")
	}
	if _, found := c.Get("other.fine"); found {
		t.Fatal("partial registration")
	}
	// Duplicate within the call.
	if err := c.Register("dup", []Definition{ok, ok}); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, found := c.Get("dup.fine"); found {
		t.Fatal("partial registration on duplicate")
	}
	// Same module again.
	if err := c.Register("mod", []Definition{a}); err == nil {
		t.Fatal("expected duplicate against catalog")
	}
	if err := c.Register("", []Definition{ok}); err == nil {
		t.Fatal("expected empty module error")
	}
}
