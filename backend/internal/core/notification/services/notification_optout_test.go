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
func newOptoutTestService(t *testing.T, optouts *fakeOptouts) (*NotificationService, *fakeDriver) {
	t.Helper()
	k := newKit(Options{})
	k.svc.SetOptouts(optouts)
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
	svc, driver := newOptoutTestService(t, &fakeOptouts{optedOut: map[string]bool{"ada@example.test": true}})

	res, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSuppressed {
		t.Fatalf("atteso suppressed, ho %q", res.Status)
	}
	if driver.sends != 0 {
		t.Fatal("un indirizzo che ha rinunciato non deve raggiungere il driver")
	}
}

func TestDispatch_OptoutLookupFailureDoesNotSend(t *testing.T) {
	// Fail-closed: se non sappiamo se ha rinunciato, non spediamo. Il costo
	// di un'email non inviata è molto minore di quello di una spedita a chi
	// aveva chiesto di non riceverne più.
	svc, driver := newOptoutTestService(t, &fakeOptouts{err: errors.New("mongo giù")})

	res, err := svc.Send(context.Background(), marketingTo("ada@example.test"))
	if err == nil {
		t.Fatal("un errore di lettura degli opt-out deve far fallire l'invio")
	}
	if res != nil && res.Status == models.StatusSent {
		t.Fatal("non deve mai risultare inviata")
	}
	if driver.sends != 0 {
		t.Fatal("il driver non deve essere raggiunto")
	}
}

func TestDispatch_TransactionalIgnoresOptouts(t *testing.T) {
	// Un reset password non è bloccabile da un opt-out marketing, e non deve
	// nemmeno pagare la query.
	opts := &fakeOptouts{optedOut: map[string]bool{"ada@example.test": true}}
	svc, driver := newOptoutTestService(t, opts)

	res, err := svc.Send(context.Background(), transactionalTo("ada@example.test"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("un transazionale deve partire, ho %q", res.Status)
	}
	if opts.calls != 0 {
		t.Fatalf("un transazionale non deve nemmeno interrogare gli opt-out, calls=%d", opts.calls)
	}
	if driver.sends != 1 {
		t.Fatalf("atteso un invio, ho %d", driver.sends)
	}
}
