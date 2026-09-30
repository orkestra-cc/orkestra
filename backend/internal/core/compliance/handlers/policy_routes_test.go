package handlers

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// registeredRoutes returns the "METHOD path" pairs register mounts on a
// fresh Huma API, sorted.
func registeredRoutes(t *testing.T, register func(huma.API, *PolicyHandler)) []string {
	t.Helper()
	api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1.0.0"))
	register(api, &PolicyHandler{})
	var out []string
	for path, item := range api.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{
			http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPut: item.Put,
			http.MethodPatch: item.Patch, http.MethodDelete: item.Delete,
		} {
			if op != nil {
				out = append(out, method+" "+path)
			}
		}
	}
	slices.Sort(out)
	return out
}

// TestPolicyRoutes_ReadWriteSplit locks the split module.go relies on: the
// read group sits behind policy.read, the write group behind policy.manage
// plus a fresh step-up. A write route that lands in the read group would be
// reachable without manage and without step-up.
func TestPolicyRoutes_ReadWriteSplit(t *testing.T) {
	reads := []string{
		"GET /v1/admin/compliance/change-requests",
		"GET /v1/admin/compliance/change-requests/{id}",
		"GET /v1/admin/compliance/policies",
		"GET /v1/admin/compliance/policies/effective",
		"GET /v1/admin/compliance/policies/{id}",
		"GET /v1/admin/compliance/policies/{id}/versions",
		"GET /v1/admin/compliance/policy-assignments",
		"GET /v1/admin/compliance/retention-classes",
		"POST /v1/admin/compliance/policies/validate",
		"POST /v1/admin/compliance/policy-assignments/{tenantId}/validate",
	}
	writes := []string{
		"DELETE /v1/admin/compliance/policies/{id}",
		"DELETE /v1/admin/compliance/policy-assignments/{tenantId}",
		"POST /v1/admin/compliance/change-requests/{id}/approve",
		"POST /v1/admin/compliance/change-requests/{id}/reject",
		"POST /v1/admin/compliance/policies",
		"PUT /v1/admin/compliance/policies/{id}",
		"PUT /v1/admin/compliance/policy-assignments/{tenantId}",
	}
	slices.Sort(reads)
	slices.Sort(writes)

	gotReads := registeredRoutes(t, RegisterPolicyReadRoutes)
	gotWrites := registeredRoutes(t, RegisterPolicyWriteRoutes)
	if !slices.Equal(gotReads, reads) {
		t.Errorf("read routes:\n got %s\nwant %s", strings.Join(gotReads, "\n     "), strings.Join(reads, "\n     "))
	}
	if !slices.Equal(gotWrites, writes) {
		t.Errorf("write routes:\n got %s\nwant %s", strings.Join(gotWrites, "\n     "), strings.Join(writes, "\n     "))
	}
	// Only GETs and the two validate POSTs (which change nothing) may be reads.
	for _, r := range gotReads {
		if !strings.HasPrefix(r, "GET ") && !strings.HasSuffix(r, "/validate") {
			t.Errorf("%s is a write in the read group", r)
		}
	}
	for _, r := range gotWrites {
		if strings.HasPrefix(r, "GET ") || strings.HasSuffix(r, "/validate") {
			t.Errorf("%s is a read in the write group", r)
		}
	}
}

// The writes that can wait for four eyes document their 202 and keep the
// default error response.
func TestPolicyRoutes_AcceptedIsDocumented(t *testing.T) {
	api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1.0.0"))
	RegisterPolicyWriteRoutes(api, &PolicyHandler{})
	paths := api.OpenAPI().Paths
	for name, op := range map[string]*huma.Operation{
		"create":   paths["/v1/admin/compliance/policies"].Post,
		"update":   paths["/v1/admin/compliance/policies/{id}"].Put,
		"assign":   paths["/v1/admin/compliance/policy-assignments/{tenantId}"].Put,
		"unassign": paths["/v1/admin/compliance/policy-assignments/{tenantId}"].Delete,
	} {
		if op.Responses["202"] == nil || op.Responses["default"] == nil {
			t.Errorf("%s responses = %v, want 202 and default", name, keysOf(op.Responses))
		}
	}
	for name, op := range map[string]*huma.Operation{
		"delete":  paths["/v1/admin/compliance/policies/{id}"].Delete,
		"approve": paths["/v1/admin/compliance/change-requests/{id}/approve"].Post,
		"reject":  paths["/v1/admin/compliance/change-requests/{id}/reject"].Post,
	} {
		if op.Responses["202"] != nil {
			t.Errorf("%s documents a 202 it never returns", name)
		}
	}
}

func keysOf(m map[string]*huma.Response) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
