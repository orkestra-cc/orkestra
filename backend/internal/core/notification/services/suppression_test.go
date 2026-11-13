package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// memSuppressions is an in-memory SuppressionRepository with the real
// normalization, so the tests prove the send path and the writers agree.
type memSuppressions struct {
	rows  map[string]models.SuppressionDoc
	err   error
	calls int
}

func newMemSuppressions(addrs ...string) *memSuppressions {
	m := &memSuppressions{rows: map[string]models.SuppressionDoc{}}
	for _, a := range addrs {
		n := repository.NormalizeSuppressionAddress(a)
		m.rows[n] = models.SuppressionDoc{Address: n, Reason: iface.SuppressionReasonHardBounce}
	}
	return m
}

func (m *memSuppressions) IsSuppressed(_ context.Context, a string) (bool, error) {
	m.calls++
	if m.err != nil {
		return false, m.err
	}
	_, ok := m.rows[repository.NormalizeSuppressionAddress(a)]
	return ok, nil
}

func (m *memSuppressions) GetMany(_ context.Context, addrs []string) (map[string]models.SuppressionDoc, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := map[string]models.SuppressionDoc{}
	for _, a := range addrs {
		n := repository.NormalizeSuppressionAddress(a)
		if d, ok := m.rows[n]; ok {
			out[n] = d
		}
	}
	return out, nil
}

func (m *memSuppressions) InsertIfAbsent(_ context.Context, addrs []string, tmpl models.SuppressionDoc) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	var added []string
	for _, a := range addrs {
		n := repository.NormalizeSuppressionAddress(a)
		if _, ok := m.rows[n]; ok || n == "" {
			continue
		}
		d := tmpl
		d.Address = n
		m.rows[n] = d
		added = append(added, n)
	}
	return added, nil
}

func (m *memSuppressions) DeleteMany(_ context.Context, addrs []string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	var removed []string
	for _, a := range addrs {
		n := repository.NormalizeSuppressionAddress(a)
		if _, ok := m.rows[n]; ok {
			delete(m.rows, n)
			removed = append(removed, n)
		}
	}
	return removed, nil
}

func (m *memSuppressions) List(_ context.Context, after string, limit int, contains, reason string) ([]models.SuppressionDoc, error) {
	if m.err != nil {
		return nil, m.err
	}
	keys := make([]string, 0, len(m.rows))
	for k := range m.rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	after = repository.NormalizeSuppressionAddress(after)
	contains = strings.ToLower(strings.TrimSpace(contains))
	var out []models.SuppressionDoc
	for _, k := range keys {
		d := m.rows[k]
		if (after != "" && k <= after) || (contains != "" && !strings.Contains(k, contains)) || (reason != "" && d.Reason != reason) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, d)
	}
	return out, nil
}

// suppressionKit: a service that can deliver both types (one-click waived,
// no opt-outs), with the suppression list and its manager over one store.
type suppressionKit struct {
	*kit
	store *memSuppressions
	mgr   iface.EmailSuppressionManager
	sink  *captureSink
}

func newSuppressionKit(t *testing.T, addrs ...string) *suppressionKit {
	t.Helper()
	k := newKit(Options{OneClickWaived: true})
	k.svc.SetOptouts(&fakeOptouts{})
	k.tmpl.tmpl = &models.TemplateDoc{TemplateID: "tpl", Locale: "en", Subject: "s"}
	k.tmpl.rendered = &Rendered{Subject: "s", BodyText: "t", BodyHTML: "<p>h</p>"}
	store := newMemSuppressions(addrs...)
	k.svc.SetSuppressions(store)
	sink := &captureSink{}
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceAuditSink, iface.AuditSink(sink))
	return &suppressionKit{kit: k, store: store, sink: sink,
		mgr: NewSuppressionManager(store, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))}
}

// channel is one way an email leaves the platform: every consumer module
// reaches the driver through one of these four.
type channel struct {
	name string
	send func(k *suppressionKit, to string) (*iface.NotificationResult, error)
}

func channels() []channel {
	send := func(typ string) func(*suppressionKit, string) (*iface.NotificationResult, error) {
		return func(k *suppressionKit, to string) (*iface.NotificationResult, error) {
			return k.svc.Send(context.Background(), iface.NotificationRequest{
				Type: typ, Category: "c", Recipients: []iface.Recipient{{Address: to}}, Subject: "S", Body: "B",
			})
		}
	}
	templated := func(typ string) func(*suppressionKit, string) (*iface.NotificationResult, error) {
		return func(k *suppressionKit, to string) (*iface.NotificationResult, error) {
			return k.svc.SendTemplated(context.Background(), iface.TemplatedNotificationRequest{
				Type: typ, Category: "c", TemplateID: "tpl", Recipients: []iface.Recipient{{Address: to}},
			})
		}
	}
	return []channel{
		{"Send/transactional", send(models.TypeTransactional)},
		{"Send/marketing", send(models.TypeMarketing)},
		{"SendTemplated/transactional", templated(models.TypeTransactional)},
		{"SendTemplated/marketing", templated(models.TypeMarketing)},
	}
}

func TestSuppression_EveryChannelWithholdsAndLiftRestores(t *testing.T) {
	for _, ch := range channels() {
		t.Run(ch.name, func(t *testing.T) {
			k := newSuppressionKit(t, "ada@example.test")

			res, err := ch.send(k, "ada@example.test")
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if res.Status != models.StatusSuppressed || res.Error != iface.SuppressedAddressInvalid {
				t.Fatalf("result = %+v, want suppressed/%s", res, iface.SuppressedAddressInvalid)
			}
			if k.driver.sends != 0 {
				t.Fatal("a suppressed address must not reach the driver")
			}
			row := k.logRepo.created[len(k.logRepo.created)-1]
			if row.Status != models.StatusSuppressed || row.Error != iface.SuppressedAddressInvalid {
				t.Fatalf("log row = %s/%s", row.Status, row.Error)
			}

			lifted, err := k.mgr.LiftSuppressions(context.Background(), []string{"ADA@example.test"}, iface.SuppressionEvidence{Source: "test"})
			if err != nil || len(lifted) != 1 {
				t.Fatalf("lift = %v, %v", lifted, err)
			}
			res, err = ch.send(k, "ada@example.test")
			if err != nil {
				t.Fatalf("send after lift: %v", err)
			}
			if res.Status != models.StatusSent || k.driver.sends != 1 {
				t.Fatalf("after lift: status %q, driver sends %d", res.Status, k.driver.sends)
			}
		})
	}
}

func TestSuppression_MatchesAnyCasingAndDisplayName(t *testing.T) {
	for _, to := range []string{"Ada@Example.TEST", "  ada@example.test ", "Ada Lovelace <ada@example.test>"} {
		k := newSuppressionKit(t, "ada@example.test")
		res, err := channels()[0].send(k, to)
		if err != nil {
			t.Fatalf("%q: %v", to, err)
		}
		if res.Status != models.StatusSuppressed || k.driver.sends != 0 {
			t.Fatalf("%q: status %q, sends %d", to, res.Status, k.driver.sends)
		}
	}
}

func TestSuppression_UnreadableListFailsClosedAndTransient(t *testing.T) {
	k := newSuppressionKit(t)
	k.store.err = errors.New("mongo down: ada@example.test")
	res, err := channels()[0].send(k, "ada@example.test")
	if err == nil || !strings.Contains(res.Error, "err=suppression_lookup_unavailable") {
		t.Fatalf("err = %v, res.Error = %q, want the bounded suppression_lookup_unavailable reason", err, res.Error)
	}
	if res.Status != models.StatusFailed || res.FailureReason != iface.FailureTransient {
		t.Fatalf("result = %+v, want failed/transient", res)
	}
	if k.driver.sends != 0 {
		t.Fatal("an unreadable list must not let the send through")
	}
}

func TestSuppression_OtherAddressesStillSend(t *testing.T) {
	k := newSuppressionKit(t, "ada@example.test")
	res, err := channels()[0].send(k, "bob@example.test")
	if err != nil || res.Status != models.StatusSent || k.driver.sends != 1 {
		t.Fatalf("res=%+v err=%v sends=%d", res, err, k.driver.sends)
	}
}

func TestSuppression_SendTestRefusesAndLiftRestores(t *testing.T) {
	k := newSuppressionKit(t, "ada@example.test")
	res, err := k.svc.SendTest(context.Background(), TestSendInput{To: "ada@example.test", Subject: "s", BodyText: "b"})
	if !errors.Is(err, ErrAddressSuppressed) || res.Diagnostic != iface.SuppressedAddressInvalid {
		t.Fatalf("SendTest = %+v, %v", res, err)
	}
	if k.driver.sends != 0 {
		t.Fatal("a test send must not reach a suppressed address")
	}
	if _, err := k.mgr.LiftSuppressions(context.Background(), []string{"ada@example.test"}, iface.SuppressionEvidence{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.svc.SendTest(context.Background(), TestSendInput{To: "ada@example.test", Subject: "s", BodyText: "b"}); err != nil {
		t.Fatalf("SendTest after lift: %v", err)
	}
	if k.driver.sends != 1 {
		t.Fatalf("driver sends = %d", k.driver.sends)
	}
}

func TestSuppressionManager_SuppressKeepsFirstRowAndAudits(t *testing.T) {
	k := newSuppressionKit(t, "old@example.test")
	ctx := context.WithValue(context.WithValue(context.Background(), ctxauth.KeyUserUUID, "op-1"), ctxauth.KeyTenantID, "t-1") //nolint:staticcheck // ctxauth exports untyped keys
	added, err := k.mgr.SuppressAddresses(ctx, []string{"New@Example.test", "old@example.test", "new@example.test", " "},
		iface.SuppressionEvidence{Reason: iface.SuppressionReasonHardBounce, Source: "operator.import"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "new@example.test" {
		t.Fatalf("added = %v", added)
	}
	row := k.store.rows["new@example.test"]
	if row.Reason != iface.SuppressionReasonHardBounce || row.Source != "operator.import" || row.ActorUUID != "op-1" || row.TenantID != "t-1" || row.CreatedAt.IsZero() {
		t.Fatalf("row = %+v", row)
	}
	if len(k.sink.events) != 1 {
		t.Fatalf("audit events = %d, want 1 (only the added address)", len(k.sink.events))
	}
	ev := k.sink.events[0]
	if ev.Action != auditActionSuppressionAdded || ev.ActorUserID != "op-1" || ev.ActorType != "user" || ev.TenantID != "t-1" || ev.Metadata["address"] != "new@example.test" {
		t.Fatalf("audit = %+v", ev)
	}

	got, err := k.mgr.LookupSuppressions(ctx, []string{"NEW@example.test", "none@example.test"})
	if err != nil || len(got) != 1 || got["new@example.test"].Reason != iface.SuppressionReasonHardBounce {
		t.Fatalf("lookup = %+v, %v", got, err)
	}
}

func TestSuppressionManager_DefaultReasonIsManual(t *testing.T) {
	k := newSuppressionKit(t)
	if _, err := k.mgr.SuppressAddresses(context.Background(), []string{"a@example.test"}, iface.SuppressionEvidence{}); err != nil {
		t.Fatal(err)
	}
	if r := k.store.rows["a@example.test"].Reason; r != iface.SuppressionReasonManual {
		t.Fatalf("reason = %q", r)
	}
	if k.sink.events[0].ActorType != "system" {
		t.Fatalf("no actor in ctx must audit as system, got %q", k.sink.events[0].ActorType)
	}
}

func TestSuppressionManager_LiftAuditsOnlyRemovedRows(t *testing.T) {
	k := newSuppressionKit(t, "a@example.test")
	lifted, err := k.mgr.LiftSuppressions(context.Background(), []string{"a@example.test", "b@example.test"}, iface.SuppressionEvidence{Source: "operator.manual"})
	if err != nil || len(lifted) != 1 {
		t.Fatalf("lifted = %v, %v", lifted, err)
	}
	if len(k.sink.events) != 1 || k.sink.events[0].Action != auditActionSuppressionLifted {
		t.Fatalf("audit = %+v", k.sink.events)
	}
	if _, ok := k.sink.events[0].Metadata["reason"]; ok {
		t.Fatal("a lift carries no reason")
	}
}

func TestSuppressionManager_BoundsBatchAndScrubsErrors(t *testing.T) {
	k := newSuppressionKit(t)
	if _, err := k.mgr.SuppressAddresses(context.Background(), []string{"a@example.test"}, iface.SuppressionEvidence{Reason: "spam"}); !errors.Is(err, ErrSuppressionReasonInvalid) {
		t.Fatalf("unknown reason: %v", err)
	}
	big := make([]string, iface.MaxSuppressionBatch+1)
	if _, err := k.mgr.SuppressAddresses(context.Background(), big, iface.SuppressionEvidence{}); !errors.Is(err, ErrSuppressionBatchTooLarge) {
		t.Fatalf("err = %v", err)
	}
	k.store.err = errors.New("E11000 dup key: secret@example.test")
	_, err := k.mgr.LookupSuppressions(context.Background(), []string{"secret@example.test"})
	if !errors.Is(err, ErrSuppressionOperationFailed) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret@example.test") {
		t.Fatalf("error leaks the address: %v", err)
	}
}

func TestSuppressionManager_ListPagesByAddressWithFilters(t *testing.T) {
	k := newSuppressionKit(t, "c@example.test", "a@example.test", "b@other.test")
	if _, err := k.mgr.SuppressAddresses(context.Background(), []string{"d@example.test"}, iface.SuppressionEvidence{}); err != nil {
		t.Fatal(err)
	}
	page, err := k.mgr.ListSuppressions(context.Background(), iface.SuppressionListQuery{Limit: 2})
	if err != nil || len(page) != 2 || page[0].Address != "a@example.test" || page[1].Address != "b@other.test" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	page, err = k.mgr.ListSuppressions(context.Background(), iface.SuppressionListQuery{AfterAddress: page[1].Address, Limit: 2})
	if err != nil || len(page) != 2 || page[0].Address != "c@example.test" || page[1].Address != "d@example.test" {
		t.Fatalf("second page = %+v, %v", page, err)
	}
	page, err = k.mgr.ListSuppressions(context.Background(), iface.SuppressionListQuery{Contains: "EXAMPLE", Reason: iface.SuppressionReasonManual})
	if err != nil || len(page) != 1 || page[0].Address != "d@example.test" || page[0].At.IsZero() {
		t.Fatalf("filtered = %+v, %v", page, err)
	}
	if _, err := k.mgr.ListSuppressions(context.Background(), iface.SuppressionListQuery{Reason: "spam"}); !errors.Is(err, ErrSuppressionReasonInvalid) {
		t.Fatalf("unknown reason: %v", err)
	}
	if len(k.sink.events) != 1 {
		t.Fatalf("a read must not audit; events = %d", len(k.sink.events))
	}
	k.store.err = errors.New("boom: secret@example.test")
	if _, err := k.mgr.ListSuppressions(context.Background(), iface.SuppressionListQuery{}); !errors.Is(err, ErrSuppressionOperationFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}
