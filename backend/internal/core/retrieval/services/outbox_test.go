package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

type probeKey struct{}

type fakeOutboxStore struct {
	calls   int
	ctx     context.Context
	intents []iface.RetrievalIntent
	now     time.Time
}

func (f *fakeOutboxStore) Enqueue(ctx context.Context, in []iface.RetrievalIntent, now time.Time) error {
	f.calls++
	f.ctx, f.intents, f.now = ctx, in, now
	return nil
}

var outboxNow = time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)

func newTestOutbox() (*Outbox, *fakeOutboxStore) {
	st := &fakeOutboxStore{}
	return NewOutbox(st, &fakeCatalog{specs: []iface.RetrievalCorpusSpec{{Name: "crm_contacts"}}}, func() time.Time { return outboxNow }), st
}

func okIntent(id string) iface.RetrievalIntent {
	return iface.RetrievalIntent{Corpus: "crm_contacts", SourceID: id, Revision: 1}
}

func TestEnqueue_PassesIntentsWithTheCallerContext(t *testing.T) {
	o, st := newTestOutbox()
	ctx := context.WithValue(tombCtx("t1"), probeKey{}, "session")
	in := okIntent("c1")
	if err := o.Enqueue(ctx, in); err != nil {
		t.Fatal(err)
	}
	if st.calls != 1 || st.ctx.Value(probeKey{}) != "session" {
		t.Fatalf("calls=%d ctx probe=%v", st.calls, st.ctx.Value(probeKey{}))
	}
	if len(st.intents) != 1 || st.intents[0] != in || !st.now.Equal(outboxNow) {
		t.Fatalf("intents=%v now=%v", st.intents, st.now)
	}
}

func TestEnqueue_EmptyIsNoop(t *testing.T) {
	o, st := newTestOutbox()
	if err := o.Enqueue(context.Background()); err != nil || st.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, st.calls)
	}
}

func TestEnqueue_RefusesExternalTenant(t *testing.T) {
	o, st := newTestOutbox()
	ctx := ctxauth.WithTenantKind(tombCtx("t1"), "external")
	err := o.Enqueue(ctx, okIntent("c1"))
	if !errors.Is(err, tenantrepo.ErrTenantKindMismatch) || st.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, st.calls)
	}
}

func TestEnqueue_RefusesUnknownCorpus(t *testing.T) {
	o, st := newTestOutbox()
	err := o.Enqueue(tombCtx("t1"), okIntent("c0"), iface.RetrievalIntent{Corpus: "nope", SourceID: "c1", Revision: 1})
	if !errors.Is(err, iface.ErrRetrievalUnknownCorpus) || st.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, st.calls)
	}
}

func TestEnqueue_RefusesInvalidIntents(t *testing.T) {
	many := make([]iface.RetrievalIntent, MaxIntentsPerEnqueue+1)
	for i := range many {
		many[i] = okIntent("c")
	}
	bad := func(id string, rev int64) []iface.RetrievalIntent {
		return []iface.RetrievalIntent{{Corpus: "crm_contacts", SourceID: id, Revision: rev}}
	}
	cases := map[string][]iface.RetrievalIntent{
		"empty id":     bad("", 1),
		"long id":      bad(strings.Repeat("a", 257), 1),
		"nul":          bad("a\x00b", 1),
		"bad utf8":     bad(string([]byte{0xff}), 1),
		"negative rev": bad("c1", -1),
		"too many":     many,
		"valid then invalid": {okIntent("c1"),
			{Corpus: "crm_contacts", SourceID: "", Revision: 1}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			o, st := newTestOutbox()
			err := o.Enqueue(tombCtx("t1"), in...)
			if !errors.Is(err, iface.ErrRetrievalInvalidIntent) || st.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, st.calls)
			}
		})
	}
	if err := ValidateIntent(okIntent(strings.Repeat("a", 256))); err != nil {
		t.Fatalf("256 bytes must be valid: %v", err)
	}
}

func TestService_IsTheRegisteredOutbox(t *testing.T) {
	s := New(Options{})
	if err := s.Enqueue(tombCtx("t1"), okIntent("c1")); err == nil {
		t.Fatal("unwired outbox must error")
	}
	o, st := newTestOutbox()
	s.SetOutbox(o)
	if err := s.Enqueue(tombCtx("t1"), okIntent("c1")); err != nil || st.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, st.calls)
	}
}
