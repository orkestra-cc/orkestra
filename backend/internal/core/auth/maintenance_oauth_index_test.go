package auth

// A deploy that skipped migration 0010 must NOT run the ownership-first
// OAuth flow without the constraint it relies on: a duplicate key is how a
// conflict is detected, and without the unique (provider, providerId) index
// there is no duplicate key. The module verifies the index at Start and
// degrades — never fails to boot: auth is a core module and StartAll hands
// its error to log.Fatalf.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// fakeIndexLister answers ListIndexNames from a map; a missing collection
// returns no names, a configured error is returned for every call.
type fakeIndexLister struct {
	names map[string][]string
	err   error
}

func (f fakeIndexLister) ListIndexNames(_ context.Context, collection string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.names[collection], nil
}

func bothIndexed() map[string][]string {
	return map[string][]string{
		models.OperatorOAuthProvidersCollection: {"_id_", "userUuid_1_provider_1", oauthIdentityIndexName},
		models.ClientOAuthProvidersCollection:   {"_id_", "userUuid_1_provider_1", oauthIdentityIndexName},
	}
}

func TestVerifyOAuthIdentityIndex_PresentOnBothIsNil(t *testing.T) {
	m := &AuthModule{indexLister: fakeIndexLister{names: bothIndexed()}}
	if err := m.verifyOAuthIdentityIndex(context.Background()); err != nil {
		t.Fatalf("both collections indexed, got %v", err)
	}
}

func TestVerifyOAuthIdentityIndex_MissingOnOneNamesThatCollection(t *testing.T) {
	names := bothIndexed()
	names[models.ClientOAuthProvidersCollection] = []string{"_id_", "userUuid_1_provider_1"}
	m := &AuthModule{indexLister: fakeIndexLister{names: names}}
	err := m.verifyOAuthIdentityIndex(context.Background())
	if err == nil {
		t.Fatal("a collection without the index must be reported")
	}
	if !strings.Contains(err.Error(), models.ClientOAuthProvidersCollection) {
		t.Fatalf("error must name the collection, got %v", err)
	}
}

func TestVerifyOAuthIdentityIndex_ListFailureIsAnError(t *testing.T) {
	m := &AuthModule{indexLister: fakeIndexLister{err: errors.New("mongo: no reachable servers")}}
	if err := m.verifyOAuthIdentityIndex(context.Background()); err == nil {
		t.Fatal("an unreadable index list must not read as present")
	}
}

func TestHealthCheck_DegradedWhileTheIdentityIndexIsMissing(t *testing.T) {
	m := &AuthModule{}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("healthy module must report nil, got %v", err)
	}
	m.oauthIndexMissing.Store(true)
	err := m.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("the health check must report degraded while the index is missing")
	}
	if !strings.Contains(err.Error(), "0010") {
		t.Fatalf("the degraded reason must point at migration 0010, got %v", err)
	}
}

// oauthStoreDegraded is what module.go wires into both tier services: the
// flag the boot check sets, read live so a module restart after the
// migration lifts the degradation without a redeploy.
func TestOAuthStoreDegraded_FollowsTheFlag(t *testing.T) {
	m := &AuthModule{}
	if m.oauthStoreDegraded() {
		t.Fatal("open by default")
	}
	m.oauthIndexMissing.Store(true)
	if !m.oauthStoreDegraded() {
		t.Fatal("degraded once the flag is set")
	}
}
