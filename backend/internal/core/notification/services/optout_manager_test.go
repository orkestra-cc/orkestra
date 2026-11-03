package services

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// memOptouts is an in-memory MarketingOptoutRepository keyed by the
// normalized address, with the same DeleteIfAt guard as the real one.
type memOptouts struct {
	docs map[string]models.MarketingOptoutDoc
}

func newMemOptouts() *memOptouts { return &memOptouts{docs: map[string]models.MarketingOptoutDoc{}} }

func (m *memOptouts) Upsert(_ context.Context, d models.MarketingOptoutDoc) error {
	a := repository.NormalizeOptoutAddress(d.Address)
	if _, ok := m.docs[a]; !ok {
		d.Address = a
		m.docs[a] = d
	}
	return nil
}
func (m *memOptouts) IsOptedOut(_ context.Context, a, _ string) (bool, error) {
	_, ok := m.docs[repository.NormalizeOptoutAddress(a)]
	return ok, nil
}
func (m *memOptouts) Get(_ context.Context, a string) (*models.MarketingOptoutDoc, error) {
	d, ok := m.docs[repository.NormalizeOptoutAddress(a)]
	if !ok {
		return nil, nil
	}
	return &d, nil
}
func (m *memOptouts) DeleteIfAt(_ context.Context, a string, at time.Time) (bool, error) {
	a = repository.NormalizeOptoutAddress(a)
	d, ok := m.docs[a]
	if !ok || !d.At.Equal(at) {
		return false, nil
	}
	delete(m.docs, a)
	return true, nil
}

// memTokens answers GetByUUID only; the rest of the interface is never
// reached by the manager.
type memTokens struct {
	repository.UnsubscribeRepository
	byUUID map[string]models.UnsubscribeTokenDoc
}

func (m *memTokens) GetByUUID(_ context.Context, u string) (*models.UnsubscribeTokenDoc, error) {
	d, ok := m.byUUID[u]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &d, nil
}

type captureSink struct{ events []iface.AuditEvent }

func (c *captureSink) Emit(_ context.Context, e iface.AuditEvent) { c.events = append(c.events, e) }

type managerKit struct {
	mgr     iface.MarketingOptoutManager
	optouts *memOptouts
	tokens  *memTokens
	sink    *captureSink
}

func newManagerKit(t *testing.T) *managerKit {
	t.Helper()
	k := &managerKit{optouts: newMemOptouts(), tokens: &memTokens{byUUID: map[string]models.UnsubscribeTokenDoc{}}, sink: &captureSink{}}
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceAuditSink, iface.AuditSink(k.sink))
	k.mgr = NewOptoutManager(k.optouts, k.tokens, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return k
}

func TestOptoutManager_RecordDoesNotFireSink(t *testing.T) {
	// The unsubscribe sink belongs to Consume. A consumer that records its own
	// opt-out already knows about it, so the manager is built without any sink
	// and its only side effects are the stored row and one audit event.
	k := newManagerKit(t)
	if err := k.mgr.RecordMarketingOptout(context.Background(), "ada@example.test", iface.OptoutEvidence{TenantID: "t1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, ok := k.optouts.docs["ada@example.test"]; !ok {
		t.Fatal("the opt-out must be stored")
	}
	if len(k.sink.events) != 1 || k.sink.events[0].Action != "notification.optout.recorded" {
		t.Fatalf("want exactly the recorded audit event, got %+v", k.sink.events)
	}
}

func TestOptoutManager_RecordStampsTenant(t *testing.T) {
	k := newManagerKit(t)
	if err := k.mgr.RecordMarketingOptout(context.Background(), "  Ada@Example.TEST ", iface.OptoutEvidence{TenantID: "t1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	d, ok := k.optouts.docs["ada@example.test"]
	if !ok {
		t.Fatal("address must be stored normalized")
	}
	if d.TenantID != "t1" || d.Category != "" || d.At.IsZero() {
		t.Fatalf("unexpected doc: %+v", d)
	}
}

func TestOptoutManager_LookupFallsBackToTokenContext(t *testing.T) {
	k := newManagerKit(t)
	at := time.Unix(100, 0)
	k.optouts.docs["ada@example.test"] = models.MarketingOptoutDoc{Address: "ada@example.test", At: at, SourceTokenUUID: "tok1"}
	k.tokens.byUUID["tok1"] = models.UnsubscribeTokenDoc{UUID: "tok1", Context: "ref-from-token"}

	info, err := k.mgr.LookupMarketingOptout(context.Background(), "ADA@example.test")
	if err != nil || info == nil {
		t.Fatalf("Lookup: %v %v", info, err)
	}
	if info.SourceContext != "ref-from-token" || !info.At.Equal(at) {
		t.Fatalf("unexpected info: %+v", info)
	}

	// A context stored on the opt-out wins over the token's.
	d := k.optouts.docs["ada@example.test"]
	d.SourceContext, d.TenantID = "own", "t9"
	k.optouts.docs["ada@example.test"] = d
	info, _ = k.mgr.LookupMarketingOptout(context.Background(), "ada@example.test")
	if info.SourceContext != "own" || info.TenantID != "t9" {
		t.Fatalf("unexpected info: %+v", info)
	}

	// An expired token leaves the context empty, not an error.
	d.SourceContext = ""
	d.SourceTokenUUID = "gone"
	k.optouts.docs["ada@example.test"] = d
	info, err = k.mgr.LookupMarketingOptout(context.Background(), "ada@example.test")
	if err != nil || info == nil || info.SourceContext != "" {
		t.Fatalf("expired token: %+v %v", info, err)
	}
}

func TestOptoutManager_LookupNoneReturnsNil(t *testing.T) {
	k := newManagerKit(t)
	info, err := k.mgr.LookupMarketingOptout(context.Background(), "nobody@example.test")
	if info != nil || err != nil {
		t.Fatalf("want nil, nil; got %v, %v", info, err)
	}
}

func TestOptoutManager_LiftDeletesOnlyMatchingAt(t *testing.T) {
	k := newManagerKit(t)
	at := time.Unix(100, 0)
	k.optouts.docs["ada@example.test"] = models.MarketingOptoutDoc{Address: "ada@example.test", At: at}

	lifted, err := k.mgr.LiftMarketingOptout(context.Background(), "ada@example.test", at.Add(time.Second), iface.OptoutEvidence{})
	if err != nil || lifted {
		t.Fatalf("wrong instant: lifted=%v err=%v", lifted, err)
	}
	if _, ok := k.optouts.docs["ada@example.test"]; !ok {
		t.Fatal("a newer opt-out must be kept")
	}
	if len(k.sink.events) != 0 {
		t.Fatal("a lift that deleted nothing must not be audited")
	}
	lifted, err = k.mgr.LiftMarketingOptout(context.Background(), "Ada@example.test", at, iface.OptoutEvidence{})
	if err != nil || !lifted {
		t.Fatalf("right instant: lifted=%v err=%v", lifted, err)
	}
	if _, ok := k.optouts.docs["ada@example.test"]; ok {
		t.Fatal("the opt-out must be gone")
	}
}

func TestOptoutManager_LiftAndRecordEmitAudit(t *testing.T) {
	k := newManagerKit(t)
	ev := iface.OptoutEvidence{TenantID: "t1", Source: "crm.consent_request", Reference: "resp-1"}
	if err := k.mgr.RecordMarketingOptout(context.Background(), "ada@example.test", ev); err != nil {
		t.Fatal(err)
	}
	at := k.optouts.docs["ada@example.test"].At
	if ok, err := k.mgr.LiftMarketingOptout(context.Background(), "ada@example.test", at, ev); err != nil || !ok {
		t.Fatalf("lift: %v %v", ok, err)
	}
	if len(k.sink.events) != 2 {
		t.Fatalf("want 2 audit events, got %d", len(k.sink.events))
	}
	for i, want := range []string{"notification.optout.recorded", "notification.optout.lifted"} {
		e := k.sink.events[i]
		if e.Action != want || e.ResourceType != "marketing_optout" || e.ActorType != "system" || e.TenantID != "t1" {
			t.Fatalf("event %d: %+v", i, e)
		}
		if e.Metadata["source"] != "crm.consent_request" || e.Metadata["reference"] != "resp-1" || e.Metadata["address"] != "ada@example.test" {
			t.Fatalf("event %d metadata: %v", i, e.Metadata)
		}
	}
}

func TestOptoutManager_MissingSinkIsNoOp(t *testing.T) {
	k := newManagerKit(t)
	k.mgr = NewOptoutManager(k.optouts, k.tokens, module.NewServiceRegistry(), nil)
	if err := k.mgr.RecordMarketingOptout(context.Background(), "ada@example.test", iface.OptoutEvidence{}); err != nil {
		t.Fatalf("a missing audit sink must not fail the record: %v", err)
	}
}

func TestOptoutManager_RecordEmptyAddressFails(t *testing.T) {
	k := newManagerKit(t)
	if err := k.mgr.RecordMarketingOptout(context.Background(), "  ", iface.OptoutEvidence{}); err == nil {
		t.Fatal("an empty address must be refused, not silently dropped")
	}
}
