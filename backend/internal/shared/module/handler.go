package module

import (
	"context"
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// ModuleAdminHandler provides Huma-compatible handlers for the admin module API.
type ModuleAdminHandler struct {
	configService *ModuleConfigService
	registry      *ModuleRegistry
}

// NewModuleAdminHandler creates a new admin handler.
func NewModuleAdminHandler(cs *ModuleConfigService, registry *ModuleRegistry) *ModuleAdminHandler {
	return &ModuleAdminHandler{configService: cs, registry: registry}
}

// --- DTOs ---

// ListModulesOutput is the response for GET /v1/admin/modules.
type ListModulesOutput struct {
	Body struct {
		Modules []ModuleConfigResponse `json:"modules"`
	}
}

// GetModuleInput is the request for GET /v1/admin/modules/{name}.
type GetModuleInput struct {
	Name string `path:"name" doc:"Module name"`
}

// GetModuleOutput is the response for GET /v1/admin/modules/{name}.
type GetModuleOutput struct {
	Body ModuleConfigResponse
}

// UpdateModuleInput is the request for PATCH /v1/admin/modules/{name}.
type UpdateModuleInput struct {
	Name string `path:"name" doc:"Module name"`
	Body struct {
		Enabled *bool             `json:"enabled,omitempty" doc:"Enable or disable the module"`
		Config  map[string]string `json:"config,omitempty" doc:"Non-secret config values to update"`
		Secrets map[string]string `json:"secrets,omitempty" doc:"Secret config values (will be encrypted)"`
	}
}

// UpdateModuleOutput is the response for PATCH /v1/admin/modules/{name}.
type UpdateModuleOutput struct {
	Body ModuleConfigResponse
}

// ModuleHealthOutput is the response for GET /v1/admin/modules/health.
type ModuleHealthOutput struct {
	Body struct {
		Modules   []ModuleHealthStatus `json:"modules"`
		CheckedAt string               `json:"checkedAt"`
	}
}

// ModuleHealthStatus represents the health of a single module.
type ModuleHealthStatus struct {
	ModuleName string `json:"moduleName"`
	Status     string `json:"status"` // "healthy" | "unhealthy" | "disabled" | "failed"
	Error      string `json:"error,omitempty"`
}

// ModuleConfigResponse is the API representation of a module config.
// Secrets are never returned — only a per-field indicator of whether a value exists.
type ModuleConfigResponse struct {
	ModuleName       string            `json:"moduleName"`
	DisplayName      string            `json:"displayName"`
	Description      string            `json:"description"`
	Category         ModuleCategory    `json:"category"`
	Enabled          bool              `json:"enabled"`
	Status           string            `json:"status"` // "running" | "failed" | "disabled"
	Error            string            `json:"error,omitempty"`
	NeedsRestart     bool              `json:"needsRestart"`
	ConfigValues     map[string]string `json:"configValues"`
	SecretStatus     map[string]bool   `json:"secretStatus"`
	ConfigSchema     []ConfigField     `json:"configSchema"`
	DependsOn        []string          `json:"dependsOn,omitempty"`
	ProvidedServices []string          `json:"providedServices,omitempty"`
	RequiredServices []string          `json:"requiredServices,omitempty"`
	OptionalServices []string          `json:"optionalServices,omitempty"`
	CreatedAt        string            `json:"createdAt"`
	UpdatedAt        string            `json:"updatedAt"`
}

// --- Handlers ---

// ListModules returns all module configurations.
func (h *ModuleAdminHandler) ListModules(ctx context.Context, _ *struct{}) (*ListModulesOutput, error) {
	configs, err := h.configService.GetAllConfigs(ctx)
	if err != nil {
		return nil, err
	}

	resp := make([]ModuleConfigResponse, len(configs))
	for i, c := range configs {
		resp[i] = h.toConfigResponse(c)
	}

	return &ListModulesOutput{
		Body: struct {
			Modules []ModuleConfigResponse `json:"modules"`
		}{Modules: resp},
	}, nil
}

// GetModule returns a single module configuration.
func (h *ModuleAdminHandler) GetModule(ctx context.Context, input *GetModuleInput) (*GetModuleOutput, error) {
	config, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
	}

	return &GetModuleOutput{Body: h.toConfigResponse(*config)}, nil
}

// UpdateModule updates a module's enabled state and/or configuration.
func (h *ModuleAdminHandler) UpdateModule(ctx context.Context, input *UpdateModuleInput) (*UpdateModuleOutput, error) {
	// Check module exists
	existing, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
	}

	// Toggle enabled state
	if input.Body.Enabled != nil {
		if err := h.configService.UpdateEnabled(ctx, input.Name, *input.Body.Enabled); err != nil {
			// Core module disable attempt returns a user-facing 400 error
			if existing.Category == CategoryCore {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, err
		}
	}

	// Update config values
	if len(input.Body.Config) > 0 || len(input.Body.Secrets) > 0 {
		// Merge with existing values (don't wipe unset fields)
		mergedValues := existing.ConfigValues
		if mergedValues == nil {
			mergedValues = make(map[string]string)
		}
		for k, v := range input.Body.Config {
			mergedValues[k] = v
		}

		if err := h.configService.UpdateConfig(ctx, input.Name, mergedValues, input.Body.Secrets); err != nil {
			return nil, err
		}
	}

	// Return updated config
	updated, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}

	return &UpdateModuleOutput{Body: h.toConfigResponse(*updated)}, nil
}

// HealthCheck runs health checks on all enabled modules.
func (h *ModuleAdminHandler) HealthCheck(ctx context.Context, _ *struct{}) (*ModuleHealthOutput, error) {
	failedModules := h.registry.FailedModules()
	enabledSet := make(map[string]bool)
	for _, name := range h.registry.EnabledModules() {
		enabledSet[name] = true
	}

	var statuses []ModuleHealthStatus
	for _, m := range h.registry.AllModules() {
		name := m.Name()

		if failErr, isFailed := failedModules[name]; isFailed {
			statuses = append(statuses, ModuleHealthStatus{
				ModuleName: name,
				Status:     "failed",
				Error:      failErr.Error(),
			})
			continue
		}

		if !enabledSet[name] {
			statuses = append(statuses, ModuleHealthStatus{
				ModuleName: name,
				Status:     "disabled",
			})
			continue
		}

		if err := m.HealthCheck(ctx); err != nil {
			statuses = append(statuses, ModuleHealthStatus{
				ModuleName: name,
				Status:     "unhealthy",
				Error:      err.Error(),
			})
		} else {
			statuses = append(statuses, ModuleHealthStatus{
				ModuleName: name,
				Status:     "healthy",
			})
		}
	}

	return &ModuleHealthOutput{
		Body: struct {
			Modules   []ModuleHealthStatus `json:"modules"`
			CheckedAt string               `json:"checkedAt"`
		}{
			Modules:   statuses,
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

// --- Helpers ---

func (h *ModuleAdminHandler) toConfigResponse(c ModuleConfig) ModuleConfigResponse {
	secretStatus := make(map[string]bool)
	for _, field := range c.ConfigSchema {
		if field.Type == FieldSecret {
			_, hasValue := c.EncryptedValues[field.Key]
			secretStatus[field.Key] = hasValue
		}
	}

	resp := ModuleConfigResponse{
		ModuleName:   c.ModuleName,
		DisplayName:  c.DisplayName,
		Description:  c.Description,
		Category:     c.Category,
		Enabled:      c.Enabled,
		NeedsRestart: c.NeedsRestart,
		ConfigValues: c.ConfigValues,
		SecretStatus: secretStatus,
		ConfigSchema: c.ConfigSchema,
		DependsOn:    c.DependsOn,
	}

	// Derive runtime status from registry state.
	// Build a quick lookup of modules that are actually loaded in this process.
	loadedModule := false
	failedModules := h.registry.FailedModules()
	for _, m := range h.registry.AllModules() {
		if m.Name() == c.ModuleName {
			loadedModule = true
			break
		}
	}

	if failErr, isFailed := failedModules[c.ModuleName]; isFailed {
		resp.Status = "failed"
		resp.Error = failErr.Error()
	} else if !c.Enabled {
		resp.Status = "disabled"
	} else if !loadedModule && c.Category != CategoryCore {
		// Module is enabled in DB but was not loaded at boot — admin toggled
		// it via the UI and needs to restart the backend for it to take effect.
		resp.Status = "pending_restart"
		resp.NeedsRestart = true
	} else {
		resp.Status = "running"
	}

	// Populate service declarations from the registered module
	for _, m := range h.registry.AllModules() {
		if m.Name() == c.ModuleName {
			for _, k := range m.ProvidedServices() {
				resp.ProvidedServices = append(resp.ProvidedServices, string(k))
			}
			for _, k := range m.RequiredServices() {
				resp.RequiredServices = append(resp.RequiredServices, string(k))
			}
			for _, k := range m.OptionalServices() {
				resp.OptionalServices = append(resp.OptionalServices, string(k))
			}
			break
		}
	}

	if !c.CreatedAt.IsZero() {
		resp.CreatedAt = c.CreatedAt.Format("2006-01-02T15:04:05Z")
	}
	if !c.UpdatedAt.IsZero() {
		resp.UpdatedAt = c.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return resp
}
