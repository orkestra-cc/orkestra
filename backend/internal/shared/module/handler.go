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
	ModuleName            string                 `json:"moduleName"`
	DisplayName           string                 `json:"displayName"`
	Description           string                 `json:"description"`
	Category              ModuleCategory         `json:"category"`
	Enabled               bool                   `json:"enabled"`
	Status                string                 `json:"status"` // "running" | "failed" | "disabled" | "stopped"
	Error                 string                 `json:"error,omitempty"`
	NeedsRestart          bool                   `json:"needsRestart"`
	ConfigValues          map[string]string      `json:"configValues"`
	SecretStatus          map[string]bool        `json:"secretStatus"`
	ConfigSchema          []ConfigField          `json:"configSchema"`
	DependsOn             []string               `json:"dependsOn,omitempty"`
	ProvidedServices      []string               `json:"providedServices,omitempty"`
	RequiredServices      []string               `json:"requiredServices,omitempty"`
	OptionalServices      []string               `json:"optionalServices,omitempty"`
	InfraContainers       []InfraContainerStatus `json:"infraContainers,omitempty"`
	ActiveEnvironment     string                 `json:"activeEnvironment"`
	AvailableEnvironments []string               `json:"availableEnvironments"`
	CreatedAt             string                 `json:"createdAt"`
	UpdatedAt             string                 `json:"updatedAt"`
}

// InfraContainerStatus describes the observed state of a Docker container
// declared by a module via InfraContainers(). Purely informational.
type InfraContainerStatus struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

// --- Environment DTOs ---

// ListEnvironmentsInput is the request for GET /v1/admin/modules/{name}/environments.
type ListEnvironmentsInput struct {
	Name string `path:"name" doc:"Module name"`
}

// ListEnvironmentsOutput is the response for GET /v1/admin/modules/{name}/environments.
type ListEnvironmentsOutput struct {
	Body struct {
		ActiveEnvironment string   `json:"activeEnvironment"`
		Environments      []string `json:"environments"`
	}
}

// GetEnvironmentInput is the request for GET /v1/admin/modules/{name}/environments/{env}.
type GetEnvironmentInput struct {
	Name string `path:"name" doc:"Module name"`
	Env  string `path:"env" doc:"Environment name"`
}

// EnvironmentConfigResponse is the API representation of a single environment's config.
type EnvironmentConfigResponse struct {
	Environment  string            `json:"environment"`
	ConfigValues map[string]string `json:"configValues"`
	SecretStatus map[string]bool   `json:"secretStatus"`
	UpdatedAt    string            `json:"updatedAt"`
}

// GetEnvironmentOutput is the response for GET /v1/admin/modules/{name}/environments/{env}.
type GetEnvironmentOutput struct {
	Body EnvironmentConfigResponse
}

// UpdateEnvironmentInput is the request for PATCH /v1/admin/modules/{name}/environments/{env}.
type UpdateEnvironmentInput struct {
	Name string `path:"name" doc:"Module name"`
	Env  string `path:"env" doc:"Environment name"`
	Body struct {
		Config  map[string]string `json:"config,omitempty" doc:"Non-secret config values to update"`
		Secrets map[string]string `json:"secrets,omitempty" doc:"Secret config values (will be encrypted)"`
	}
}

// UpdateEnvironmentOutput is the response for PATCH /v1/admin/modules/{name}/environments/{env}.
type UpdateEnvironmentOutput struct {
	Body EnvironmentConfigResponse
}

// SetActiveEnvironmentInput is the request for PUT /v1/admin/modules/{name}/active-environment.
type SetActiveEnvironmentInput struct {
	Name string `path:"name" doc:"Module name"`
	Body struct {
		Environment string `json:"environment" doc:"Environment name to activate"`
	}
}

// SetActiveEnvironmentOutput is the response for PUT /v1/admin/modules/{name}/active-environment.
type SetActiveEnvironmentOutput struct {
	Body struct {
		ActiveEnvironment string `json:"activeEnvironment"`
		NeedsRestart      bool   `json:"needsRestart"`
	}
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
// When enabling/disabling, the module is started/stopped at runtime — no restart needed.
func (h *ModuleAdminHandler) UpdateModule(ctx context.Context, input *UpdateModuleInput) (*UpdateModuleOutput, error) {
	// Check module exists
	existing, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
	}

	// Toggle enabled state with runtime Start/Stop
	if input.Body.Enabled != nil {
		if *input.Body.Enabled {
			// --- ENABLING ---
			if err := h.configService.UpdateEnabled(ctx, input.Name, true); err != nil {
				if existing.Category == CategoryCore {
					return nil, huma.Error400BadRequest(err.Error())
				}
				return nil, err
			}

			// If module previously failed Init, retry before starting.
			failedModules := h.registry.FailedModules()
			if _, isFailed := failedModules[input.Name]; isFailed {
				if err := h.registry.RetryInit(input.Name); err != nil {
					return nil, huma.Error422UnprocessableEntity(
						fmt.Sprintf("module %q init failed: %s", input.Name, err.Error()),
					)
				}
			}

			// Start the module's background jobs.
			if err := h.registry.StartModule(ctx, input.Name); err != nil {
				return nil, huma.Error422UnprocessableEntity(
					fmt.Sprintf("module %q failed to start: %s", input.Name, err.Error()),
				)
			}

			_ = h.configService.ClearNeedsRestart(ctx, input.Name)

		} else {
			// --- DISABLING ---
			if existing.Category == CategoryCore {
				return nil, huma.Error400BadRequest("core modules cannot be disabled")
			}

			// Check dependency constraints before disabling.
			if err := h.registry.CheckCanDisable(input.Name); err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}

			if err := h.configService.UpdateEnabled(ctx, input.Name, false); err != nil {
				return nil, err
			}

			// Stop the module's background jobs.
			if err := h.registry.StopModule(ctx, input.Name); err != nil {
				// Log but don't fail — the module is disabled regardless.
				fmt.Printf("warning: module %s stop error: %v\n", input.Name, err)
			}

			_ = h.configService.ClearNeedsRestart(ctx, input.Name)
		}
	}

	// Update config values
	configChanged := false
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
		configChanged = true
	}

	// Modules that read config lazily don't need a restart for config-only changes.
	if configChanged && input.Body.Enabled == nil && h.registry.SupportsHotReload(input.Name) {
		_ = h.configService.ClearNeedsRestart(ctx, input.Name)
	}

	// Return updated config
	updated, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}

	return &UpdateModuleOutput{Body: h.toConfigResponse(*updated)}, nil
}

// HealthCheck runs health checks on all started modules.
func (h *ModuleAdminHandler) HealthCheck(ctx context.Context, _ *struct{}) (*ModuleHealthOutput, error) {
	failedModules := h.registry.FailedModules()

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

		if !h.registry.IsStarted(name) {
			statuses = append(statuses, ModuleHealthStatus{
				ModuleName: name,
				Status:     "disabled",
			})
			continue
		}

		if err := CheckHealth(ctx, m); err != nil {
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

// --- Environment Handlers ---

// ListEnvironments returns the available environments for a module.
func (h *ModuleAdminHandler) ListEnvironments(ctx context.Context, input *ListEnvironmentsInput) (*ListEnvironmentsOutput, error) {
	config, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
	}

	return &ListEnvironmentsOutput{
		Body: struct {
			ActiveEnvironment string   `json:"activeEnvironment"`
			Environments      []string `json:"environments"`
		}{
			ActiveEnvironment: config.ActiveEnv(),
			Environments:      config.AvailableEnvironments(),
		},
	}, nil
}

// GetEnvironment returns config values for a specific environment.
func (h *ModuleAdminHandler) GetEnvironment(ctx context.Context, input *GetEnvironmentInput) (*GetEnvironmentOutput, error) {
	envConfig, secretStatus, err := h.configService.GetEnvironmentConfig(ctx, input.Name, input.Env)
	if err != nil {
		return nil, huma.Error404NotFound(err.Error())
	}

	updatedAt := ""
	if !envConfig.UpdatedAt.IsZero() {
		updatedAt = envConfig.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return &GetEnvironmentOutput{
		Body: EnvironmentConfigResponse{
			Environment:  input.Env,
			ConfigValues: envConfig.ConfigValues,
			SecretStatus: secretStatus,
			UpdatedAt:    updatedAt,
		},
	}, nil
}

// UpdateEnvironment updates config values for a specific environment.
func (h *ModuleAdminHandler) UpdateEnvironment(ctx context.Context, input *UpdateEnvironmentInput) (*UpdateEnvironmentOutput, error) {
	if err := h.configService.UpdateEnvironmentConfig(ctx, input.Name, input.Env, input.Body.Config, input.Body.Secrets); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	if h.registry.SupportsHotReload(input.Name) {
		_ = h.configService.ClearNeedsRestart(ctx, input.Name)
	}

	envConfig, secretStatus, err := h.configService.GetEnvironmentConfig(ctx, input.Name, input.Env)
	if err != nil {
		return nil, err
	}

	updatedAt := ""
	if !envConfig.UpdatedAt.IsZero() {
		updatedAt = envConfig.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return &UpdateEnvironmentOutput{
		Body: EnvironmentConfigResponse{
			Environment:  input.Env,
			ConfigValues: envConfig.ConfigValues,
			SecretStatus: secretStatus,
			UpdatedAt:    updatedAt,
		},
	}, nil
}

// SetActiveEnvironment switches the active environment for a module.
func (h *ModuleAdminHandler) SetActiveEnvironment(ctx context.Context, input *SetActiveEnvironmentInput) (*SetActiveEnvironmentOutput, error) {
	if err := h.configService.SetActiveEnvironment(ctx, input.Name, input.Body.Environment); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	needsRestart := !h.registry.SupportsHotReload(input.Name)
	if !needsRestart {
		_ = h.configService.ClearNeedsRestart(ctx, input.Name)
	}

	return &SetActiveEnvironmentOutput{
		Body: struct {
			ActiveEnvironment string `json:"activeEnvironment"`
			NeedsRestart      bool   `json:"needsRestart"`
		}{
			ActiveEnvironment: input.Body.Environment,
			NeedsRestart:      needsRestart,
		},
	}, nil
}

// --- Helpers ---

func (h *ModuleAdminHandler) toConfigResponse(c ModuleConfig) ModuleConfigResponse {
	// Build secret status from the active environment's encrypted values.
	encryptedValues := c.ActiveEncryptedValues()
	secretStatus := make(map[string]bool)
	for _, field := range c.ConfigSchema {
		if field.Type == FieldSecret {
			_, hasValue := encryptedValues[field.Key]
			secretStatus[field.Key] = hasValue
		}
	}

	// Use active environment's config values for the response.
	configValues := c.ActiveConfigValues()

	resp := ModuleConfigResponse{
		ModuleName:            c.ModuleName,
		DisplayName:           c.DisplayName,
		Description:           c.Description,
		Category:              c.Category,
		Enabled:               c.Enabled,
		NeedsRestart:          c.NeedsRestart,
		ConfigValues:          configValues,
		SecretStatus:          secretStatus,
		ConfigSchema:          c.ConfigSchema,
		DependsOn:             c.DependsOn,
		ActiveEnvironment:     c.ActiveEnv(),
		AvailableEnvironments: c.AvailableEnvironments(),
	}

	// Derive runtime status from registry state.
	failedModules := h.registry.FailedModules()

	if failErr, isFailed := failedModules[c.ModuleName]; isFailed {
		resp.Status = "failed"
		resp.Error = failErr.Error()
	} else if !c.Enabled {
		resp.Status = "disabled"
	} else if h.registry.IsStarted(c.ModuleName) {
		resp.Status = "running"
	} else {
		// Enabled but not started (e.g. init succeeded but Start not called yet).
		resp.Status = "stopped"
	}

	// Populate service declarations from the registered module
	for _, m := range h.registry.AllModules() {
		if m.Name() != c.ModuleName {
			continue
		}
		for _, k := range ProvidedServicesOf(m) {
			resp.ProvidedServices = append(resp.ProvidedServices, string(k))
		}
		for _, k := range RequiredServicesOf(m) {
			resp.RequiredServices = append(resp.RequiredServices, string(k))
		}
		for _, k := range OptionalServicesOf(m) {
			resp.OptionalServices = append(resp.OptionalServices, string(k))
		}
		resp.InfraContainers = h.collectInfraStatus(m)
		break
	}

	if !c.CreatedAt.IsZero() {
		resp.CreatedAt = c.CreatedAt.Format("2006-01-02T15:04:05Z")
	}
	if !c.UpdatedAt.IsZero() {
		resp.UpdatedAt = c.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return resp
}

// collectInfraStatus queries the container manager for the running state
// of every container declared by the module. Returns nil when the module
// declares no containers (the most common case).
func (h *ModuleAdminHandler) collectInfraStatus(m Module) []InfraContainerStatus {
	specs := InfraContainersOf(m)
	if len(specs) == 0 {
		return nil
	}
	mgr := h.registry.ContainerManager()
	out := make([]InfraContainerStatus, 0, len(specs))
	for _, spec := range specs {
		status := InfraContainerStatus{Name: spec.Name, Image: spec.Image}
		if mgr != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			running, err := mgr.IsRunning(ctx, spec.Name)
			cancel()
			if err != nil {
				status.Error = err.Error()
			} else {
				status.Running = running
			}
		}
		out = append(out, status)
	}
	return out
}
