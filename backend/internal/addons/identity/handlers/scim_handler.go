package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/orkestra-cc/orkestra-addon-identity/scim"
)

// ScimHandler owns the `/scim/v2/*` surface. Implemented as raw
// net/http handlers (rather than Huma operations) because SCIM clients
// speak their own envelope format — ListResponse, Error — that doesn't
// round-trip cleanly through Huma's auto-generated body validation.
//
// v1 behaviour:
//   - ServiceProviderConfig, Schemas, ResourceTypes: static documents
//     that accurately describe what we support (i.e. "no patch, no bulk,
//     no filter, single auth scheme").
//   - /Users and /Groups: 200 + empty ListResponse on GET collection
//     and 501 on every other verb.
type ScimHandler struct{}

// NewScimHandler constructs the stub handler. Zero collaborators — it
// holds no state, persistence will be added in a follow-up commit.
func NewScimHandler() *ScimHandler { return &ScimHandler{} }

// Mount attaches the SCIM routes to the given router. The caller is
// expected to wrap this router with the BearerMiddleware before exposing
// it publicly — see module.go.
func (h *ScimHandler) Mount(r chi.Router) {
	r.Get("/ServiceProviderConfig", h.serviceProviderConfig)
	r.Get("/ResourceTypes", h.resourceTypes)
	r.Get("/Schemas", h.schemas)

	// Users
	r.Get("/Users", h.listEmpty)
	r.Post("/Users", h.notImplemented)
	r.Get("/Users/{id}", h.notImplemented)
	r.Put("/Users/{id}", h.notImplemented)
	r.Patch("/Users/{id}", h.notImplemented)
	r.Delete("/Users/{id}", h.notImplemented)

	// Groups
	r.Get("/Groups", h.listEmpty)
	r.Post("/Groups", h.notImplemented)
	r.Get("/Groups/{id}", h.notImplemented)
	r.Put("/Groups/{id}", h.notImplemented)
	r.Patch("/Groups/{id}", h.notImplemented)
	r.Delete("/Groups/{id}", h.notImplemented)
}

func (h *ScimHandler) listEmpty(w http.ResponseWriter, _ *http.Request) {
	scim.ScimJSON(w, http.StatusOK, scim.EmptyList())
}

func (h *ScimHandler) notImplemented(w http.ResponseWriter, _ *http.Request) {
	scim.ScimNotImplemented(w, "SCIM provisioning is not yet implemented (stub endpoint)")
}

func (h *ScimHandler) serviceProviderConfig(w http.ResponseWriter, _ *http.Request) {
	cfg := scim.ServiceProviderConfig{
		Schemas:          []string{scim.SchemaServiceProviderConfig},
		DocumentationURI: "https://github.com/orkestra/backend/blob/main/docs/scim.md",
		Patch:            scim.SupportedFeature{Supported: false},
		Bulk:             scim.SupportedBulk{Supported: false},
		Filter:           scim.SupportedFilter{Supported: false},
		ChangePassword:   scim.SupportedFeature{Supported: false},
		Sort:             scim.SupportedFeature{Supported: false},
		ETag:             scim.SupportedFeature{Supported: false},
		AuthenticationSchemes: []scim.AuthenticationScheme{{
			Type:        "oauthbearertoken",
			Name:        "OAuth Bearer Token",
			Description: "Per-tenant long-lived bearer token; rotate via POST /v1/identity/scim/rotate-token",
			SpecURI:     "https://tools.ietf.org/html/rfc6750",
			Primary:     true,
		}},
		Meta: &scim.Meta{ResourceType: "ServiceProviderConfig"},
	}
	scim.ScimJSON(w, http.StatusOK, cfg)
}

func (h *ScimHandler) resourceTypes(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	types := []scim.ResourceType{
		{
			Schemas:     []string{scim.SchemaResourceType},
			ID:          "User",
			Name:        "User",
			Endpoint:    "/Users",
			Description: "User Account",
			Schema:      scim.SchemaUser,
			Meta:        &scim.Meta{ResourceType: "ResourceType", Created: now},
		},
		{
			Schemas:     []string{scim.SchemaResourceType},
			ID:          "Group",
			Name:        "Group",
			Endpoint:    "/Groups",
			Description: "Group",
			Schema:      scim.SchemaGroup,
			Meta:        &scim.Meta{ResourceType: "ResourceType", Created: now},
		},
	}
	scim.ScimJSON(w, http.StatusOK, scim.ListResponse{
		Schemas:      []string{scim.SchemaListResponse},
		TotalResults: len(types),
		StartIndex:   1,
		ItemsPerPage: len(types),
		Resources:    toAny(types),
	})
}

func (h *ScimHandler) schemas(w http.ResponseWriter, _ *http.Request) {
	// Ship only the core User + Group schemas in v1. Enterprise User +
	// custom extensions are follow-ups.
	userSchema := scim.Schema{
		Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
		ID:          scim.SchemaUser,
		Name:        "User",
		Description: "User Account",
		Attributes: []scim.SchemaAttribute{
			{Name: "userName", Type: "string", Required: true, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "server"},
			{Name: "name", Type: "complex", Mutability: "readWrite", Returned: "default"},
			{Name: "emails", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default"},
			{Name: "active", Type: "boolean", Mutability: "readWrite", Returned: "default"},
		},
		Meta: &scim.Meta{ResourceType: "Schema"},
	}
	groupSchema := scim.Schema{
		Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
		ID:          scim.SchemaGroup,
		Name:        "Group",
		Description: "Group",
		Attributes: []scim.SchemaAttribute{
			{Name: "displayName", Type: "string", Required: true, Mutability: "readWrite", Returned: "default"},
			{Name: "members", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default"},
		},
		Meta: &scim.Meta{ResourceType: "Schema"},
	}
	scim.ScimJSON(w, http.StatusOK, scim.ListResponse{
		Schemas:      []string{scim.SchemaListResponse},
		TotalResults: 2,
		StartIndex:   1,
		ItemsPerPage: 2,
		Resources:    []any{userSchema, groupSchema},
	})
}

// toAny converts any slice into []any for embedding in ListResponse.
// SCIM clients tolerate heterogeneous Resources arrays, so this is safer
// than defining a per-resource generic list envelope.
func toAny[T any](items []T) []any {
	out := make([]any, len(items))
	for i, v := range items {
		out[i] = v
	}
	return out
}
