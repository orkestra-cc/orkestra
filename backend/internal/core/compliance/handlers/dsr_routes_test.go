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

func mountedRoutes(register func(huma.API)) []string {
	api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1.0.0"))
	register(api)
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

// TestAdminDSRRouteSplit pins which register function mounts which route;
// module.go guards the read functions with audit.read and the write functions
// with dsr.manage + step-up (see TestAdminDSRRoutesAreGuarded), so a write
// that lands in a read function would be reachable without either.
func TestAdminDSRRouteSplit(t *testing.T) {
	cases := []struct {
		name     string
		register func(huma.API)
		want     []string
	}{
		{"erasure reads", func(a huma.API) { RegisterErasureRequestAdminReadRoutes(a, &ErasureRequestHandler{}) }, []string{
			"GET /v1/admin/compliance/erasure-requests",
			"GET /v1/admin/compliance/erasure-requests/{id}/receipt",
		}},
		{"erasure writes", func(a huma.API) { RegisterErasureRequestAdminWriteRoutes(a, &ErasureRequestHandler{}) }, []string{
			"POST /v1/admin/compliance/erasure-requests",
			"POST /v1/admin/compliance/erasure-requests/{id}/approve",
			"POST /v1/admin/compliance/erasure-requests/{id}/close",
			"POST /v1/admin/compliance/erasure-requests/{id}/execute",
			"POST /v1/admin/compliance/erasure-requests/{id}/reject",
		}},
		{"export reads", func(a huma.API) { RegisterDSRExportReadRoutes(a, &DSRExportHandler{}) }, []string{
			"GET /v1/admin/compliance/dsr/exports",
		}},
		{"export writes", func(a huma.API) { RegisterDSRExportWriteRoutes(a, &DSRExportHandler{}) }, []string{
			"GET /v1/admin/compliance/dsr/exports/{id}/download",
			"POST /v1/admin/compliance/dsr/exports",
		}},
		{"preview", func(a huma.API) { RegisterDSRPreviewRoutes(a, &DSRPreviewHandler{}) }, []string{
			"GET /v1/admin/compliance/dsr/preview",
		}},
	}
	for _, c := range cases {
		got := mountedRoutes(c.register)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, strings.Join(got, "\n     "), strings.Join(c.want, "\n     "))
		}
	}
}
