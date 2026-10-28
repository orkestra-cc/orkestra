package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// SectionCatalog is the slice of module.UserPreferenceRegistry the profile
// section routes read.
type SectionCatalog interface {
	SectionsFor(surface string) []module.ProfileSectionSpec
}

// PreferenceHandler serves the self-service preference store and the
// profile-section discovery routes. One instance serves both tiers: the
// caller's UUID comes from the token, and the store is user-keyed.
type PreferenceHandler struct {
	prefs    services.PreferenceService
	sections SectionCatalog
	enabled  services.ModuleEnabledChecker
	// authz resolves the caller's permissions for the section filter; nil
	// (no provider wired) keeps every section — a discovery list, not a
	// gate: the section's own routes still enforce the permission.
	authz iface.AuthzProvider
}

// NewPreferenceHandler builds the handler. enabled and authz may be nil.
func NewPreferenceHandler(prefs services.PreferenceService, sections SectionCatalog, enabled services.ModuleEnabledChecker, authz iface.AuthzProvider) *PreferenceHandler {
	if enabled == nil {
		enabled = func(context.Context, string) bool { return true }
	}
	return &PreferenceHandler{prefs: prefs, sections: sections, enabled: enabled, authz: authz}
}

// --- preferences ----------------------------------------------------------

type ModulePreferencesResponse struct {
	Body services.ModulePreferences
}

type AllPreferencesResponse struct {
	Body struct {
		Modules []*services.ModulePreferences `json:"modules"`
	}
}

type GetMyPreferencesRequest struct {
	Module string `path:"module" pattern:"^[a-z][a-z0-9_]*$" maxLength:"64" doc:"Module name"`
}

type PutMyPreferencesRequest struct {
	Module string `path:"module" pattern:"^[a-z][a-z0-9_]*$" maxLength:"64" doc:"Module name"`
	Body   struct {
		Values map[string]string `json:"values" doc:"Flat key → value map; every key must be a field the module declares"`
	}
}

func (h *PreferenceHandler) caller(ctx context.Context) (string, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return "", huma.Error401Unauthorized("authentication required")
	}
	return userUUID, nil
}

func mapPreferenceError(err error) error {
	switch {
	case errors.Is(err, services.ErrPreferenceModuleUnknown):
		return errcode.NotFound(errcode.UserPreferencesModuleUnknown, "This module declares no preferences or is disabled")
	case errors.Is(err, services.ErrPreferenceInvalid):
		var verrs module.ValueErrors
		detail := "One or more values are invalid"
		if errors.As(err, &verrs) {
			parts := make([]string, len(verrs))
			for i, e := range verrs {
				parts[i] = e.Error()
			}
			detail = strings.Join(parts, "; ")
		}
		return errcode.UnprocessableEntity(errcode.UserPreferencesInvalid, detail)
	}
	return huma.Error500InternalServerError("Failed to access preferences", err)
}

// ListMyPreferences handles GET /v1/me/preferences.
func (h *PreferenceHandler) ListMyPreferences(ctx context.Context, _ *struct{}) (*AllPreferencesResponse, error) {
	userUUID, err := h.caller(ctx)
	if err != nil {
		return nil, err
	}
	all, err := h.prefs.GetAll(ctx, userUUID)
	if err != nil {
		return nil, mapPreferenceError(err)
	}
	out := &AllPreferencesResponse{}
	out.Body.Modules = all
	if out.Body.Modules == nil {
		out.Body.Modules = []*services.ModulePreferences{}
	}
	return out, nil
}

// GetMyPreferences handles GET /v1/me/preferences/{module}.
func (h *PreferenceHandler) GetMyPreferences(ctx context.Context, req *GetMyPreferencesRequest) (*ModulePreferencesResponse, error) {
	userUUID, err := h.caller(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.prefs.Get(ctx, userUUID, req.Module)
	if err != nil {
		return nil, mapPreferenceError(err)
	}
	return &ModulePreferencesResponse{Body: *p}, nil
}

// PutMyPreferences handles PUT /v1/me/preferences/{module}.
func (h *PreferenceHandler) PutMyPreferences(ctx context.Context, req *PutMyPreferencesRequest) (*ModulePreferencesResponse, error) {
	userUUID, err := h.caller(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.prefs.Put(ctx, userUUID, req.Module, req.Body.Values)
	if err != nil {
		return nil, mapPreferenceError(err)
	}
	return &ModulePreferencesResponse{Body: *p}, nil
}

// --- profile sections -----------------------------------------------------

type ProfileSectionsResponse struct {
	Body struct {
		Surface  string                      `json:"surface" enum:"self,admin"`
		Sections []module.ProfileSectionSpec `json:"sections"`
	}
}

type ProfileSectionsForUserRequest struct {
	ID string `path:"id" doc:"Target user UUID"`
}

// visibleSections filters the declared sections of a surface to enabled
// modules and, when an authz provider is wired, to the ones whose
// Permission the caller holds at global scope.
func (h *PreferenceHandler) visibleSections(ctx context.Context, callerUUID, surface string) []module.ProfileSectionSpec {
	out := []module.ProfileSectionSpec{}
	if h.sections == nil {
		return out
	}
	for _, s := range h.sections.SectionsFor(surface) {
		if !h.enabled(ctx, s.Module) {
			continue
		}
		if s.Permission != "" && h.authz != nil {
			ok, err := h.authz.HasPermission(ctx, callerUUID, "", s.Permission)
			if err != nil || !ok {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// MyProfileSections handles GET /v1/me/profile-sections.
func (h *PreferenceHandler) MyProfileSections(ctx context.Context, _ *struct{}) (*ProfileSectionsResponse, error) {
	userUUID, err := h.caller(ctx)
	if err != nil {
		return nil, err
	}
	out := &ProfileSectionsResponse{}
	out.Body.Surface = module.ProfileSurfaceSelf
	out.Body.Sections = h.visibleSections(ctx, userUUID, module.ProfileSurfaceSelf)
	return out, nil
}

// ProfileSectionsForUser handles GET /v1/users/{id}/profile-sections: the
// sections an administrator sees on another user's profile. The target id
// is accepted for symmetry with the profile routes; the list depends on the
// caller (permissions) and on module state, not on the target.
func (h *PreferenceHandler) ProfileSectionsForUser(ctx context.Context, _ *ProfileSectionsForUserRequest) (*ProfileSectionsResponse, error) {
	userUUID, err := h.caller(ctx)
	if err != nil {
		return nil, err
	}
	out := &ProfileSectionsResponse{}
	out.Body.Surface = module.ProfileSurfaceAdmin
	out.Body.Sections = h.visibleSections(ctx, userUUID, module.ProfileSurfaceAdmin)
	return out, nil
}

// RegisterSelfPreferenceRoutes mounts the self-service routes. Called once
// per audience surface (operator and client) under RequirePermission("user.self").
func RegisterSelfPreferenceRoutes(api huma.API, h *PreferenceHandler, idPrefix string) {
	huma.Register(api, huma.Operation{
		OperationID: idPrefix + "list-my-preferences",
		Method:      http.MethodGet,
		Path:        "/v1/me/preferences",
		Summary:     "List my preferences for every module that declares them",
		Description: "One entry per enabled module with a preference schema: the declared fields and the caller's values, defaults filled in.",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ListMyPreferences)
	huma.Register(api, huma.Operation{
		OperationID: idPrefix + "get-my-preferences",
		Method:      http.MethodGet,
		Path:        "/v1/me/preferences/{module}",
		Summary:     "Get my preferences for one module",
		Description: "404 user.preferences.module_unknown when the module declares no preferences or is disabled.",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetMyPreferences)
	huma.Register(api, huma.Operation{
		OperationID: idPrefix + "put-my-preferences",
		Method:      http.MethodPut,
		Path:        "/v1/me/preferences/{module}",
		Summary:     "Replace my preferences for one module",
		Description: "PUT semantics: the stored values become exactly the submitted map; absent fields fall back to their declared default. Every key must be a declared field and every value must satisfy its type, options, min/max and pattern (422 user.preferences.invalid, listing every problem).",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.PutMyPreferences)
	huma.Register(api, huma.Operation{
		OperationID: idPrefix + "my-profile-sections",
		Method:      http.MethodGet,
		Path:        "/v1/me/profile-sections",
		Summary:     "Profile sections modules contribute to my own profile",
		Description: "Declared by modules (module.HasProfileSections), filtered to enabled modules and to the sections whose permission the caller holds.",
		Tags:        []string{"Users", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.MyProfileSections)
}

// RegisterProfileSectionReadRoute mounts the admin-surface discovery route,
// in the user.read group.
func RegisterProfileSectionReadRoute(api huma.API, h *PreferenceHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "user-profile-sections",
		Method:      http.MethodGet,
		Path:        "/v1/users/{id}/profile-sections",
		Summary:     "Profile sections modules contribute to a user's admin profile",
		Description: "Declared by modules (module.HasProfileSections) for the admin surface, filtered to enabled modules and to the sections whose permission the caller holds.",
		Tags:        []string{"Users"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ProfileSectionsForUser)
}
