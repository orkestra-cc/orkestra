package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type fakeOptouts struct {
	optedOut map[string]bool
	err      error
	calls    int
}

func (f *fakeOptouts) Upsert(context.Context, models.MarketingOptoutDoc) error { return nil }
func (f *fakeOptouts) IsOptedOut(_ context.Context, address, _ string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.optedOut[address], nil
}

// newOptoutTestService reuses newKit's skeleton (resolver, driver, logRepo,
// prefService) and wires the given fake opt-out repository via SetOptouts,
// exactly the way module.go wires the real one.
func newOptoutTestKit(t *testing.T, optouts *fakeOptouts) *kit {
	t.Helper()
	k := newKit(Options{})
	k.svc.SetOptouts(optouts)
	return k
}

func newOptoutTestService(t *testing.T, optouts *fakeOptouts) (*NotificationService, *fakeDriver) {
	t.Helper()
	k := newOptoutTestKit(t, optouts)
	return k.svc, k.driver
}

func marketingTo(addr string) iface.NotificationRequest {
	return iface.NotificationRequest{
		Type:       models.TypeMarketing,
		Recipients: []iface.Recipient{{Address: addr}},
		Subject:    "S",
		Body:       "B",
	}
}

func transactionalTo(addr string) iface.NotificationRequest {
	return iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: addr}},
		Subject:    "S",
		Body:       "B",
	}
}

func TestDispatch_MarketingToAnOptedOutAddressIsSuppressed(t *testing.T) {
	k := newOptoutTestKit(t, &fakeOptouts{optedOut: map[string]bool{"ada@example.test": true}})

	res, err := k.svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSuppressed {
		t.Fatalf("expected suppressed, got %q", res.Status)
	}
	if k.driver.sends != 0 {
		t.Fatal("an opted-out address must not reach the driver")
	}
	// The delivery-log row is the only place an operator can tell a
	// suppressed-by-opt-out send apart from a suppressed-by-preference or
	// suppressed-by-suppression-list one, so the marker is part of the
	// contract, not an implementation detail.
	if len(k.logRepo.created) != 1 {
		t.Fatalf("want exactly one delivery-log row, got %d", len(k.logRepo.created))
	}
	if got := k.logRepo.created[0].Error; got != "marketing_optout" {
		t.Fatalf("delivery-log Error = %q, want marketing_optout", got)
	}
}

func TestDispatch_OptoutLookupFailureDoesNotSend(t *testing.T) {
	// Fail-closed: if we don't know whether the address opted out, we don't
	// send. The cost of an email not sent is far lower than the cost of one
	// sent to someone who asked to stop receiving them.
	svc, driver := newOptoutTestService(t, &fakeOptouts{err: errors.New("mongo down")})

	res, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err == nil {
		t.Fatal("an opt-out lookup error must fail the send")
	}
	if res != nil && res.Status == models.StatusSent {
		t.Fatal("must never come back as sent")
	}
	if driver.sends != 0 {
		t.Fatal("the driver must not be reached")
	}
}

func TestDispatch_TransactionalIgnoresOptouts(t *testing.T) {
	// A password reset cannot be blocked by a marketing opt-out, and must not
	// even pay for the query.
	opts := &fakeOptouts{optedOut: map[string]bool{"ada@example.test": true}}
	svc, driver := newOptoutTestService(t, opts)

	res, err := svc.Send(context.Background(), transactionalTo("ada@example.test"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("a transactional send must go through, got %q", res.Status)
	}
	if opts.calls != 0 {
		t.Fatalf("a transactional send must not even query opt-outs, calls=%d", opts.calls)
	}
	if driver.sends != 1 {
		t.Fatalf("expected one send, got %d", driver.sends)
	}
}
