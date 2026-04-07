package module

import (
	"context"
	"fmt"
)

// ModuleAdminHandler provides Huma-compatible handlers for the admin module API.
type ModuleAdminHandler struct {
	configService *ModuleConfigService
}

// NewModuleAdminHandler creates a new admin handler.
func NewModuleAdminHandler(cs *ModuleConfigService) *ModuleAdminHandler {
	return &ModuleAdminHandler{configService: cs}
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

// ModuleConfigResponse is the API representation of a module config.
// Secrets are never returned — only a per-field indicator of whether a value exists.
type ModuleConfigResponse struct {
	ModuleName   string            `json:"moduleName"`
	DisplayName  string            `json:"displayName"`
	Description  string            `json:"description"`
	Category     ModuleCategory    `json:"category"`
	Enabled      bool              `json:"enabled"`
	NeedsRestart bool              `json:"needsRestart"`
	ConfigValues map[string]string `json:"configValues"`
	SecretStatus map[string]bool   `json:"secretStatus"` // key → true if a value is stored
	ConfigSchema []ConfigField     `json:"configSchema"`
	DependsOn    []string          `json:"dependsOn,omitempty"`
	CreatedAt    string            `json:"createdAt"`
	UpdatedAt    string            `json:"updatedAt"`
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
		resp[i] = toConfigResponse(c)
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
		return nil, fmt.Errorf("%w: module %q not found", ErrModuleNotFound, input.Name)
	}

	return &GetModuleOutput{Body: toConfigResponse(*config)}, nil
}

// UpdateModule updates a module's enabled state and/or configuration.
func (h *ModuleAdminHandler) UpdateModule(ctx context.Context, input *UpdateModuleInput) (*UpdateModuleOutput, error) {
	// Check module exists
	existing, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("%w: module %q not found", ErrModuleNotFound, input.Name)
	}

	// Toggle enabled state
	if input.Body.Enabled != nil {
		if err := h.configService.UpdateEnabled(ctx, input.Name, *input.Body.Enabled); err != nil {
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

	return &UpdateModuleOutput{Body: toConfigResponse(*updated)}, nil
}

// --- Errors ---

// ErrModuleNotFound is returned when a module config doesn't exist.
var ErrModuleNotFound = fmt.Errorf("module not found")

// --- Helpers ---

func toConfigResponse(c ModuleConfig) ModuleConfigResponse {
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

	if !c.CreatedAt.IsZero() {
		resp.CreatedAt = c.CreatedAt.Format("2006-01-02T15:04:05Z")
	}
	if !c.UpdatedAt.IsZero() {
		resp.UpdatedAt = c.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return resp
}

