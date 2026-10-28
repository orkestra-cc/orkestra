package repository

import (
	"errors"
	"testing"
	"time"
)

func TestTenantProgress_ClaimBuildAdvanceResume(t *testing.T) {
	db := newTestDB(t)
	ensureIndexes(t, db, CollTenantProgress, TenantProgressIndexes())
	p := NewTenantProgress(db)
	t1, t2 := tctx("t1"), tctx("t2")
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	lease := time.Minute

	c, err := p.ClaimBuild(t1, "crm_contacts", 1, "w1", now, lease)
	if err != nil || c == nil || c.Status != "building" || c.Cursor != "" || c.Epoch != 1 {
		t.Fatalf("first claim = %+v, %v", c, err)
	}
	if c, err := p.ClaimBuild(t1, "crm_contacts", 1, "w2", now, lease); err != nil || c != nil {
		t.Fatalf("held claim = %+v, %v", c, err)
	}
	if err := p.Advance(t1, "crm_contacts", 1, 1, "w1", "c200", 200, false, now); err != nil {
		t.Fatal(err)
	}
	c, err = p.ClaimBuild(t1, "crm_contacts", 1, "w2", now, lease)
	if err != nil || c == nil || c.Cursor != "c200" || c.Enqueued != 200 || c.Epoch != 2 {
		t.Fatalf("resume claim = %+v, %v", c, err)
	}
	if err := p.Advance(t1, "crm_contacts", 1, 1, "w1", "x", 1, false, now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale advance = %v", err)
	}
	if err := p.Advance(t1, "crm_contacts", 1, 2, "w2", "", 50, true, now); err != nil {
		t.Fatal(err)
	}
	g, err := p.Get(t1, "crm_contacts", 1)
	if err != nil || g == nil || g.Status != "done" || g.Enqueued != 250 || g.DoneAt == nil || g.LeaseUntil != nil {
		t.Fatalf("done row = %+v, %v", g, err)
	}
	if c, err := p.ClaimBuild(t1, "crm_contacts", 1, "w3", now.Add(time.Hour), lease); err != nil || c != nil {
		t.Fatalf("claim after done = %+v, %v", c, err)
	}
	if g, err := p.Get(t2, "crm_contacts", 1); err != nil || g != nil {
		t.Fatalf("other tenant Get = %+v, %v", g, err)
	}
}
