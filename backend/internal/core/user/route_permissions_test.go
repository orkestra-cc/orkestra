package user

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/user/handlers"
)

// moduleWithStubHandlers builds a module whose handlers exist but whose
// services are nil. Route registration only needs the method values;
// nothing here dispatches a request.
func moduleWithStubHandlers() *UserModule {
	return &UserModule{
		handler:            handlers.NewUserHandler(nil),
		adminClientHandler: handlers.NewAdminClientUserHandler(nil, nil),
		preferenceHandler:  handlers.NewPreferenceHandler(nil, nil, nil, nil),
	}
}

type gate struct {
	Permission string
	StepUp     bool
}

// operationsPerGate registers each route group onto its own throwaway API
// and reads back the operations that landed, so the assertions describe
// the surface as Huma actually mounts it rather than as the source reads.
func operationsPerGate(t *testing.T) map[gate][]string {
	t.Helper()
	out := map[gate][]string{}
	for _, g := range moduleWithStubHandlers().routeGroups() {
		api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1.0.0"))
		g.Register(api)
		k := gate{g.Permission, g.StepUp}
		for path, item := range api.OpenAPI().Paths {
			for method, op := range map[string]*huma.Operation{
				http.MethodGet:    item.Get,
				http.MethodPost:   item.Post,
				http.MethodPatch:  item.Patch,
				http.MethodPut:    item.Put,
				http.MethodDelete: item.Delete,
			} {
				if op != nil {
					out[k] = append(out[k], method+" "+path)
				}
			}
		}
		sort.Strings(out[k])
	}
	return out
}

func gateOf(t *testing.T, ops map[gate][]string, op string) (gate, bool) {
	t.Helper()
	for g, list := range ops {
		for _, candidate := range list {
			if candidate == op {
				return g, true
			}
		}
	}
	return gate{}, false
}

// The whole surface used to sit behind system.users.admin. Read must mean
// read: nothing but a GET rides on user.read.
func TestRouteGroups_NoMutationBehindReadPermission(t *testing.T) {
	for g, list := range operationsPerGate(t) {
		if g.Permission != "user.read" {
			continue
		}
		if g.StepUp {
			t.Errorf("user.read must not require step-up")
		}
		for _, op := range list {
			if !strings.HasPrefix(op, "GET ") {
				t.Errorf("%s is gated by user.read — a mutation must not ride on the read permission", op)
			}
		}
	}
}

// Every mutation sits on its own verb key, and the irreversible ones (role
// change, deactivate, delete) also behind a fresh step-up proof. The two
// tiers share the same keys (D6).
func TestRouteGroups_VerbsHaveTheirOwnPermission(t *testing.T) {
	ops := operationsPerGate(t)
	want := map[string]gate{
		"GET /v1/users":                                        {"user.read", false},
		"GET /v1/users/{id}":                                   {"user.read", false},
		"GET /v1/users/count":                                  {"user.read", false},
		"GET /v1/admin/client-users":                           {"user.read", false},
		"GET /v1/users/{id}/profile-sections":                  {"user.read", false},
		"POST /v1/users":                                       {"user.create", false},
		"POST /v1/admin/client-users":                          {"user.create", false},
		"POST /v1/admin/client-users/invite":                   {"user.create", false},
		"POST /v1/admin/client-users/{id}/invite/resend":       {"user.create", false},
		"POST /v1/admin/client-users/{id}/resend-verification": {"user.create", false},
		"POST /v1/admin/client-users/{id}/send-password-reset": {"user.update", false}, // D3
		"PUT /v1/users/{id}":                                   {"user.update", true},
		"PATCH /v1/admin/client-users/{id}":                    {"user.update", true},
		"DELETE /v1/users/{id}":                                {"user.delete", true},
		"DELETE /v1/admin/client-users/{id}":                   {"user.delete", true},
	}
	for op, wantGate := range want {
		got, ok := gateOf(t, ops, op)
		if !ok {
			t.Errorf("%s is not mounted by any route group", op)
			continue
		}
		if got != wantGate {
			t.Errorf("%s is gated by %+v, want %+v", op, got, wantGate)
		}
	}
}

// system.users.admin is no longer a gate on this surface (D5): the keys
// the module declares are the keys its routes use, and every declared
// admin key gates something. user.self and user.avatar.self are the two
// self-service keys mounted elsewhere (avatar routes) or reserved.
func TestRouteGroups_DeclaredKeysMatchMountedKeys(t *testing.T) {
	ops := operationsPerGate(t)
	mounted := map[string]bool{}
	for g, list := range ops {
		if g.Permission == "system.users.admin" {
			t.Errorf("system.users.admin still gates %v — the user surface is per-action now", list)
		}
		if len(list) > 0 {
			mounted[g.Permission] = true
		}
	}
	for _, spec := range moduleWithStubHandlers().Permissions() {
		switch spec.Key {
		case "user.self", "user.avatar.self":
			continue
		}
		if !mounted[spec.Key] {
			t.Errorf("permission %q is declared but gates no route", spec.Key)
		}
	}
	declared := map[string]bool{}
	for _, spec := range moduleWithStubHandlers().Permissions() {
		declared[spec.Key] = true
	}
	for perm := range mounted {
		if !declared[perm] {
			t.Errorf("routes are gated by %q, which Permissions() does not declare", perm)
		}
	}
}

// Each group becomes its own chi sub-router. Registering the same
// method+path in two of them would panic at boot.
func TestRouteGroups_NoOperationRegisteredTwice(t *testing.T) {
	seen := map[string]gate{}
	for g, list := range operationsPerGate(t) {
		for _, op := range list {
			if prev, dup := seen[op]; dup {
				t.Errorf("%s is registered under both %+v and %+v — chi panics on a duplicate route", op, prev, g)
			}
			seen[op] = g
		}
	}
	if len(seen) == 0 {
		t.Fatal("no operations collected — the harness is not exercising route registration")
	}
}

// The self-service preference routes are mounted per audience under
// user.self (global scope on the operator mount, tenant scope on the client
// mount), outside the admin groups: none of them may
// appear in routeGroups, and the registrar mounts exactly the four self ops.
func TestSelfPreferenceRoutes_AreNotInTheAdminGroupsAndMountFourOps(t *testing.T) {
	for g, list := range operationsPerGate(t) {
		for _, op := range list {
			if strings.Contains(op, "/v1/me/") {
				t.Errorf("%s is mounted in admin group %+v — self routes live on user.self", op, g)
			}
		}
	}
	api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1.0.0"))
	handlers.RegisterSelfPreferenceRoutes(api, handlers.NewPreferenceHandler(nil, nil, nil, nil), "operator-")
	var ops []string
	for path, item := range api.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{http.MethodGet: item.Get, http.MethodPut: item.Put} {
			if op != nil {
				ops = append(ops, method+" "+path)
			}
		}
	}
	sort.Strings(ops)
	want := []string{
		"GET /v1/me/preferences",
		"GET /v1/me/preferences/{module}",
		"GET /v1/me/profile-sections",
		"PUT /v1/me/preferences/{module}",
	}
	if strings.Join(ops, ",") != strings.Join(want, ",") {
		t.Errorf("self preference ops = %v, want %v", ops, want)
	}
}
