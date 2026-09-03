package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ModuleAdminHandler provides Huma-compatible handlers for the admin module API.
type ModuleAdminHandler struct {
	configService *ModuleConfigService
	registry      *ModuleRegistry
	auditSink     iface.AuditSink
	actorResolver func(context.Context) AdminActor
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
	ModuleName  string         `json:"moduleName"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description"`
	Category    ModuleCategory `json:"category"`
	Enabled     bool           `json:"enabled"`
	Status      string         `json:"status"` // "running" | "failed" | "disabled" | "stopped" | "missing"
	Error       string         `json:"error,omitempty"`
	// Missing is true for a required module whose config document is absent
	// (see ModuleConfigService.RequirePersistedConfig). Status is "missing",
	// ConfigValues/SecretStatus are empty, and every mutation on the module
	// returns 503 until the document is restored or the backend restarted.
	Missing               bool                   `json:"missing,omitempty"`
	NeedsRestart          bool                   `json:"needsRestart"`
	ConfigValues          map[string]string      `json:"configValues"`
	SecretStatus          map[string]bool        `json:"secretStatus"`
	ConfigSchema          []ConfigField          `json:"configSchema"`
	ConfigGroups          []ConfigGroup          `json:"configGroups,omitempty"`
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
	// Revision is what a subsequent PATCH echoes back to remove record-list
	// elements. Plain int64 here — a response always carries one.
	Revision int64 `json:"revision"`
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
		Config      map[string]string       `json:"config,omitempty" doc:"Non-secret config values to update"`
		Secrets     map[string]string       `json:"secrets,omitempty" doc:"Secret config values (will be encrypted)"`
		RecordLists []RecordListMutationDTO `json:"recordLists,omitempty" doc:"Record-list membership changes"`
		// Pointer so an omitted revision is distinguishable from an explicit
		// 0 — and 0 is a legitimate expectation for a document written before
		// record lists existed.
		Revision *int64 `json:"revision,omitempty" doc:"Environment revision; required when any recordLists entry removes elements"`
	}
}

// RecordListMutationDTO is one field's membership intent on the wire. The
// wire shape and the service type (RecordListMutation) are deliberately
// separate: this one carries Huma's json/doc tags, and the service must not
// depend on the transport's tagging rules.
type RecordListMutationDTO struct {
	Field  string   `json:"field" doc:"Record-list field key"`
	Create []string `json:"create,omitempty" doc:"Slugs being created"`
	Remove []string `json:"remove,omitempty" doc:"Slugs being removed"`
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
	statuses, err := h.configService.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	resp := make([]ModuleConfigResponse, 0, len(statuses))
	for _, st := range statuses {
		if st.Missing {
			resp = append(resp, h.missingConfigResponse(st.Name))
			continue
		}
		resp = append(resp, h.toConfigResponse(*st.Config))
	}
	return &ListModulesOutput{
		Body: struct {
			Modules []ModuleConfigResponse `json:"modules"`
		}{Modules: resp},
	}, nil
}

// missingConfigResponse renders a required module whose document is gone:
// identity and schema come from the registered module (the binary is the
// source of truth for those), state is "missing".
func (h *ModuleAdminHandler) missingConfigResponse(name string) ModuleConfigResponse {
	resp := ModuleConfigResponse{
		ModuleName:            name,
		DisplayName:           name,
		Status:                "missing",
		Missing:               true,
		ConfigValues:          map[string]string{},
		SecretStatus:          map[string]bool{},
		ActiveEnvironment:     "production",
		AvailableEnvironments: DefaultEnvironments,
	}
	for _, m := range h.registry.AllModules() {
		if m.Name() != name {
			continue
		}
		resp.DisplayName = DisplayNameOf(m)
		resp.Description = DescriptionOf(m)
		resp.Category = m.Category()
		resp.ConfigSchema = ConfigSchemaOf(m)
		resp.ConfigGroups = ConfigGroupsOf(m)
		resp.DependsOn = DependenciesOf(m)
		resp.Enabled = m.Category() == CategoryCore || h.registry.IsStarted(name)
		break
	}
	return resp
}

// GetModule returns a single module configuration.
func (h *ModuleAdminHandler) GetModule(ctx context.Context, input *GetModuleInput) (*GetModuleOutput, error) {
	config, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, mapConfigReadError(err)
	}
	if config == nil {
		return nil, huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
	}

	return &GetModuleOutput{Body: h.toConfigResponse(*config)}, nil
}

// UpdateModule updates a module's configuration and/or enabled state.
//
// Order is the substance: the config half — candidate validation AND the
// compare-and-swap write — completes before any lifecycle side effect, so a
// 422, a stale-revision 409 or an infrastructure failure can never still
// start or stop the module. If config succeeds and the later lifecycle step
// fails, the config stays changed and the two audit events report those
// distinct actual results; the response is still an error.
func (h *ModuleAdminHandler) UpdateModule(ctx context.Context, input *UpdateModuleInput) (*UpdateModuleOutput, error) {
	existing, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		h.auditAborted(ctx, input, err)
		return nil, mapConfigReadError(err)
	}
	if existing == nil {
		notFound := huma.Error404NotFound(fmt.Sprintf("module %q not found", input.Name))
		h.auditAborted(ctx, input, notFound)
		return nil, notFound
	}

	configChanged := false
	if len(input.Body.Config) > 0 || len(input.Body.Secrets) > 0 {
		// UpdateConfig merges into the stored config — keys the caller omits
		// are preserved, so a config-only change never wipes the module's secrets.
		// UpdateActiveConfig, not UpdateConfig: the profile above is from a
		// SECOND read, and an activation landing between the two would file
		// this change under the wrong one. env is "" only when the document
		// could not be read, and emitAudit omits an empty env.
		env, err := h.configService.UpdateActiveConfig(ctx, input.Name, input.Body.Config, input.Body.Secrets)
		h.emitAudit(ctx, auditRecord{
			action: ActionModuleConfigUpdated, module: input.Name, env: env,
			config: input.Body.Config, secrets: input.Body.Secrets, err: err,
		})
		if err != nil {
			// Same mapping as UpdateEnvironment: a record-list error is a 409
			// or a 422 the client can act on, never the blanket 500 an
			// unmapped error becomes.
			return nil, mapConfigServiceError(err, func(e error) error {
				if code := recordListStatus(e); code != 0 {
					return huma.NewError(code, e.Error())
				}
				return e
			})
		}
		configChanged = true
	}

	if input.Body.Enabled != nil {
		// The config half persisted needsRestart=true because this module
		// reads config only at Init; the runtime start/stop does not re-run
		// Init, so the hint must survive.
		keepNeedsRestart := configChanged && !h.registry.SupportsHotReload(input.Name)
		if *input.Body.Enabled {
			err = h.enableModule(ctx, input.Name, existing, keepNeedsRestart)
		} else {
			err = h.disableModule(ctx, input.Name, existing, keepNeedsRestart)
		}
		if err != nil {
			return nil, err
		}
	}

	updated, err := h.configService.GetConfig(ctx, input.Name)
	if err != nil {
		return nil, mapConfigReadError(err)
	}
	return &UpdateModuleOutput{Body: h.toConfigResponse(*updated)}, nil
}

// auditAborted records a mutation that reached the handler but could not be
// dispatched — the document could not be read, or the module does not exist.
// One failure event per intended half, so G7's "every mutation that reaches
// the handler" holds on the abort path too.
func (h *ModuleAdminHandler) auditAborted(ctx context.Context, input *UpdateModuleInput, err error) {
	if len(input.Body.Config) > 0 || len(input.Body.Secrets) > 0 {
		h.emitAudit(ctx, auditRecord{
			action: ActionModuleConfigUpdated, module: input.Name,
			config: input.Body.Config, secrets: input.Body.Secrets, err: err,
		})
	}
	if input.Body.Enabled != nil {
		action := ActionModuleDisabled
		if *input.Body.Enabled {
			action = ActionModuleEnabled
		}
		h.emitAudit(ctx, auditRecord{action: action, module: input.Name, err: err})
	}
}

// enableModule persists enabled=true, retries a failed Init, starts the
// module, and audits the actual result. keepNeedsRestart carries the config
// half's verdict: a start is not a re-Init, so it may not clear a hint the
// config write just earned.
func (h *ModuleAdminHandler) enableModule(ctx context.Context, name string, existing *ModuleConfig, keepNeedsRestart bool) error {
	err := h.doEnable(ctx, name, existing, keepNeedsRestart)
	h.emitAudit(ctx, auditRecord{action: ActionModuleEnabled, module: name, err: err})
	return err
}

func (h *ModuleAdminHandler) doEnable(ctx context.Context, name string, existing *ModuleConfig, keepNeedsRestart bool) error {
	if err := h.configService.UpdateEnabled(ctx, name, true); err != nil {
		if existing.Category == CategoryCore {
			return huma.Error400BadRequest(err.Error())
		}
		return err
	}
	if _, isFailed := h.registry.FailedModules()[name]; isFailed {
		if err := h.registry.RetryInit(name); err != nil {
			return huma.Error422UnprocessableEntity(fmt.Sprintf("module %q init failed: %s", name, err.Error()))
		}
	}
	// Read the revision this half is allowed to retract a hint for, AFTER
	// the config half (whose write moved it) and before the lifecycle step.
	// A read failure only costs the clear, never the enable.
	revision, ok := h.observedRevision(ctx, name)
	if err := h.registry.StartModule(ctx, name); err != nil {
		return huma.Error422UnprocessableEntity(fmt.Sprintf("module %q failed to start: %s", name, err.Error()))
	}
	if !keepNeedsRestart && ok {
		h.clearNeedsRestart(ctx, name, revision)
	}
	return nil
}

// observedRevision reads the configRevision this request is entitled to
// clear needsRestart against. A failed read is reported, not fatal: the
// lifecycle change already happened (or is about to), and losing a
// presentation-flag clear is strictly better than clearing one blind.
func (h *ModuleAdminHandler) observedRevision(ctx context.Context, name string) (int64, bool) {
	doc, err := h.configService.GetConfig(ctx, name)
	if err != nil || doc == nil {
		h.logger().Info("needsRestart kept: the module config could not be re-read",
			slog.String("module", name))
		return 0, false
	}
	return doc.ConfigRevision, true
}

// clearNeedsRestart retracts the hint only while the document is still at
// the revision this request observed. Losing that compare-and-swap means a
// config change landed in between and earned the hint for itself — an
// ordinary outcome, logged and never an error.
func (h *ModuleAdminHandler) clearNeedsRestart(ctx context.Context, name string, revision int64) {
	won, err := h.configService.ClearNeedsRestartAt(ctx, name, revision)
	switch {
	case err != nil:
		h.logger().Info("needsRestart kept: the clear failed",
			slog.String("module", name), slog.String("error", err.Error()))
	case !won:
		h.logger().Info("needsRestart kept: the configuration changed concurrently",
			slog.String("module", name))
	}
}

// disableModule persists enabled=false, stops the module, and audits the
// actual result — including the rollback when the stop fails, which is a
// module.disabled/failure event derived from the returned error.
func (h *ModuleAdminHandler) disableModule(ctx context.Context, name string, existing *ModuleConfig, keepNeedsRestart bool) error {
	err := h.doDisable(ctx, name, existing, keepNeedsRestart)
	h.emitAudit(ctx, auditRecord{action: ActionModuleDisabled, module: name, err: err})
	return err
}

func (h *ModuleAdminHandler) doDisable(ctx context.Context, name string, existing *ModuleConfig, keepNeedsRestart bool) error {
	if existing.Category == CategoryCore {
		return huma.Error400BadRequest("core modules cannot be disabled")
	}
	if err := h.registry.CheckCanDisable(name); err != nil {
		return huma.Error409Conflict(err.Error())
	}
	if err := h.configService.UpdateEnabled(ctx, name, false); err != nil {
		return err
	}
	revision, ok := h.observedRevision(ctx, name)
	if err := h.registry.StopModule(ctx, name); err != nil {
		// The module is NOT disabled: StopModule returns before clearing
		// `started`, so the module — and any infra it declared — keeps
		// running. Persisting enabled=false and answering 200 would state
		// the opposite of what happened, and the next boot would then skip
		// a module the operator believes is merely stopped. Roll the flag
		// back and fail; disableModule audits module.disabled/failure from
		// the returned error. needsRestart is deliberately NOT cleared —
		// nothing was retracted.
		h.logger().Error("module stop failed; restoring enabled=true",
			slog.String("module", name), slog.String("error", err.Error()))
		if restoreErr := h.configService.UpdateEnabled(ctx, name, true); restoreErr != nil {
			h.logger().Error("module stop failed AND enabled=true could not be restored; the document now says disabled while the module runs",
				slog.String("module", name),
				slog.String("error", restoreErr.Error()),
				slog.String("stopError", err.Error()))
		}
		return huma.Error422UnprocessableEntity(fmt.Sprintf("module %q failed to stop: %s", name, err.Error()))
	}
	if !keepNeedsRestart && ok {
		h.clearNeedsRestart(ctx, name, revision)
	}
	return nil
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
		return nil, mapConfigReadError(err)
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
		// A required-module outage is not "no such environment": it must win
		// over the blanket 404 so the SPA renders it as retryable.
		if errors.Is(err, ErrRequiredConfigMissing) {
			return nil, mapConfigReadError(err)
		}
		return nil, huma.Error404NotFound(err.Error())
	}

	updatedAt := ""
	if !envConfig.UpdatedAt.IsZero() {
		updatedAt = envConfig.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return &GetEnvironmentOutput{
		Body: EnvironmentConfigResponse{
			Environment:  input.Env,
			ConfigValues: nonSecretValues(h.schemaFor(input.Name), envConfig.ConfigValues),
			SecretStatus: secretStatus,
			UpdatedAt:    updatedAt,
			Revision:     envConfig.Revision,
		},
	}, nil
}

// UpdateEnvironment updates config values for a specific environment.
func (h *ModuleAdminHandler) UpdateEnvironment(ctx context.Context, input *UpdateEnvironmentInput) (*UpdateEnvironmentOutput, error) {
	mutations := make([]RecordListMutation, 0, len(input.Body.RecordLists))
	for _, m := range input.Body.RecordLists {
		mutations = append(mutations, RecordListMutation{Field: m.Field, Create: m.Create, Remove: m.Remove})
	}

	err := h.configService.UpdateEnvironmentConfigWithRecordLists(
		ctx, input.Name, input.Env,
		input.Body.Config, input.Body.Secrets, mutations, input.Body.Revision,
	)
	h.emitAudit(ctx, auditRecord{
		action: ActionModuleConfigUpdated, module: input.Name, env: input.Env,
		config: input.Body.Config, secrets: input.Body.Secrets, recordLists: mutations, err: err,
	})
	if err != nil {
		// mapConfigServiceError owns the ConfigValidationError case: a
		// code-bearing validator gets the stable 422 envelope, a code-less
		// one keeps the text-only 422. Everything else falls through to the
		// record-list mapping — 409 means the client acted on a view of the
		// world that no longer holds; 422 means the request itself is
		// malformed. Both are more actionable than the blanket 400.
		return nil, mapConfigServiceError(err, func(e error) error {
			if code := recordListStatus(e); code != 0 {
				return huma.NewError(code, e.Error())
			}
			return huma.Error400BadRequest(e.Error())
		})
	}

	envConfig, secretStatus, err := h.configService.GetEnvironmentConfig(ctx, input.Name, input.Env)
	if err != nil {
		return nil, mapConfigReadError(err)
	}

	updatedAt := ""
	if !envConfig.UpdatedAt.IsZero() {
		updatedAt = envConfig.UpdatedAt.Format("2006-01-02T15:04:05Z")
	}

	return &UpdateEnvironmentOutput{
		Body: EnvironmentConfigResponse{
			Environment:  input.Env,
			ConfigValues: nonSecretValues(h.schemaFor(input.Name), envConfig.ConfigValues),
			SecretStatus: secretStatus,
			UpdatedAt:    updatedAt,
			Revision:     envConfig.Revision,
		},
	}, nil
}

// recordListStatus maps a record-list error to its HTTP status, or 0 when the
// error is not one. The split is by what the client can do about it: a 409
// means the roster moved under them and they should re-read and retry; a 422
// means the request could never have succeeded as sent.
func recordListStatus(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrRevisionStale), errors.Is(err, ErrSlugExists),
		errors.Is(err, ErrSlugMissing), errors.Is(err, ErrUnknownSlug):
		return http.StatusConflict
	case errors.Is(err, ErrRevisionRequired), errors.Is(err, ErrDuplicateMutationField),
		errors.Is(err, ErrCreateRemoveOverlap), errors.Is(err, ErrRosterFull),
		errors.Is(err, ErrLabelRequired), errors.Is(err, ErrLabelTooLong),
		errors.Is(err, ErrSlugLabelMismatch):
		return http.StatusUnprocessableEntity
	default:
		return 0
	}
}

// SetActiveEnvironment switches the active environment for a module.
func (h *ModuleAdminHandler) SetActiveEnvironment(ctx context.Context, input *SetActiveEnvironmentInput) (*SetActiveEnvironmentOutput, error) {
	err := h.configService.SetActiveEnvironment(ctx, input.Name, input.Body.Environment)
	h.emitAudit(ctx, auditRecord{action: ActionModuleEnvironmentActivated, module: input.Name, env: input.Body.Environment, err: err})
	if err != nil {
		return nil, mapConfigServiceError(err, func(e error) error { return huma.Error400BadRequest(e.Error()) })
	}

	// The activation write already persisted this same value; it is recomputed
	// here only to report it back.
	needsRestart := !h.registry.SupportsHotReload(input.Name)

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

// schemaForResponse is the schema a response is filtered against: the
// registered module's live declaration, or — for a document whose module is
// not registered (a fork's removed addon still in module_configs) — the
// stored snapshot, so a secret declared before the module left is still
// recognised as one.
func (h *ModuleAdminHandler) schemaForResponse(c ModuleConfig) []ConfigField {
	if live := h.schemaFor(c.ModuleName); live != nil {
		return live
	}
	return c.ConfigSchema
}

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

	// Use active environment's config values for the response — minus any
	// plaintext a legacy document still carries under a schema-declared
	// secret key, which no admin read may echo.
	configValues := nonSecretValues(h.schemaForResponse(c), c.ActiveConfigValues())

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
		// Groups are resolved live rather than read from the persisted doc:
		// they are presentation-only and never written to module_configs.
		resp.ConfigGroups = ConfigGroupsOf(m)
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
