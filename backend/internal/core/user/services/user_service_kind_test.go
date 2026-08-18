package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ListByKind extends the package's shared fakeUserRepo (defined in
// user_service_test.go) so it keeps satisfying repository.UserRepository
// once ListByKind is added to that interface. Filters the in-memory map by
// Kind — mirrors the mongoUserRepository.ListByKind query shape.
func (r *fakeUserRepo) ListByKind(_ context.Context, kind string) ([]iface.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []iface.User
	for _, u := range r.users {
		if u.Kind == kind {
			out = append(out, *u)
		}
	}
	return out, nil
}

func TestCreateUserWithPasswordPersistsKind(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	created, err := svc.CreateUserWithPassword(context.Background(), &iface.CreateUserInput{
		Email: "sa-x@service.invalid", FullName: "X", Role: "guest",
		Kind: iface.UserKindService,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Kind != iface.UserKindService {
		t.Fatalf("Kind = %q, want service", created.Kind)
	}
}
