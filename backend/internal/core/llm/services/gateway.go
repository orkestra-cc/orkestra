package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"regexp"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// Request limits (spec, "Contratto iface").
const (
	maxMessages        = 256
	maxMessagesBytes   = 2 << 20 // 2 MiB serialized
	maxTools           = 64
	maxToolSchemaBytes = 256 << 10
	maxEmbedInputs     = 256
	maxEmbedInputBytes = 64 << 10
	// PR 1 carry-over (T7.b): the spec bounds only messages and tool
	// schemas; instructions, tool descriptions and the response schema get
	// matching caps so no field of a request is unbounded.
	maxInstructionsBytes    = 256 << 10
	maxToolDescriptionBytes = 16 << 10
	maxResponseNameBytes    = 64
)

var (
	// keyRE is the closed pattern for caller and purpose keys (<= 64 chars).
	keyRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	// toolNameRE admits the mixed-case names consumers give tools
	// (getWeather); each adapter narrows it to what its provider accepts.
	toolNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
)

// Gateway implements iface.LLMGateway: validation and access here, then
// the Router (budget reservation, breaker, fallback, ledger). Until the
// module wires a router (SetRouter), Chat/Embed stop at
// ErrLLMProviderUnavailable after access and validation.
type Gateway struct {
	access   *AccessResolver
	catalog  *CatalogService
	registry *providers.Registry
	vault    *Vault
	router   *Router
	logger   *slog.Logger
}

func NewGateway(access *AccessResolver, catalog *CatalogService, registry *providers.Registry, vault *Vault, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{access: access, catalog: catalog, registry: registry, vault: vault, logger: logger}
}

// SetRouter wires the execution path; called once by the module's Init
// before any request is served.
func (g *Gateway) SetRouter(r *Router) { g.router = r }

// SetKMSProvider forwards to the vault; SetAuditSink to the catalog. They are
// the late-wiring seams compliance uses (iface.KMSProviderSetter,
// iface.AuditSinkSetter).
func (g *Gateway) SetKMSProvider(k iface.KMSProvider) { g.vault.SetKMSProvider(k) }
func (g *Gateway) SetAuditSink(s iface.AuditSink)     { g.catalog.SetAuditSink(s) }

var (
	_ iface.LLMGateway        = (*Gateway)(nil)
	_ iface.KMSProviderSetter = (*Gateway)(nil)
	_ iface.AuditSinkSetter   = (*Gateway)(nil)
)

// ValidateChatRequest enforces the contract limits before any resolution.
func ValidateChatRequest(req iface.ChatRequest) error {
	if !keyRE.MatchString(req.Caller) {
		return fmt.Errorf("%w: caller", iface.ErrLLMInvalidRequest)
	}
	if req.Purpose != "" && !keyRE.MatchString(req.Purpose) {
		return fmt.Errorf("%w: purpose", iface.ErrLLMInvalidRequest)
	}
	if len(req.Messages) == 0 || len(req.Messages) > maxMessages {
		return fmt.Errorf("%w: messages count", iface.ErrLLMInvalidRequest)
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "developer", "user", "assistant", "tool":
		default:
			return fmt.Errorf("%w: message role %q", iface.ErrLLMInvalidRequest, m.Role)
		}
		if m.Role == "tool" && m.ToolCallID == "" {
			return fmt.Errorf("%w: tool message without toolCallId", iface.ErrLLMInvalidRequest)
		}
	}
	if b, _ := json.Marshal(req.Messages); len(b) > maxMessagesBytes {
		return fmt.Errorf("%w: messages too large", iface.ErrLLMInvalidRequest)
	}
	if len(req.Instructions) > maxInstructionsBytes {
		return fmt.Errorf("%w: instructions too large", iface.ErrLLMInvalidRequest)
	}
	if len(req.Tools) > maxTools {
		return fmt.Errorf("%w: tools count", iface.ErrLLMInvalidRequest)
	}
	for _, t := range req.Tools {
		if !toolNameRE.MatchString(t.Name) {
			return fmt.Errorf("%w: tool name %q", iface.ErrLLMInvalidRequest, t.Name)
		}
		if len(t.Description) > maxToolDescriptionBytes {
			return fmt.Errorf("%w: tool description too large", iface.ErrLLMInvalidRequest)
		}
		b, err := json.Marshal(t.InputSchema)
		if err != nil {
			return fmt.Errorf("%w: tool schema not serializable", iface.ErrLLMInvalidRequest)
		}
		if len(b) > maxToolSchemaBytes {
			return fmt.Errorf("%w: tool schema too large", iface.ErrLLMInvalidRequest)
		}
	}
	if rf := req.ResponseFormat; rf != nil {
		if len(rf.Name) > maxResponseNameBytes {
			return fmt.Errorf("%w: response format name too long", iface.ErrLLMInvalidRequest)
		}
		b, err := json.Marshal(rf.Schema)
		if err != nil {
			return fmt.Errorf("%w: response schema not serializable", iface.ErrLLMInvalidRequest)
		}
		if len(b) > maxToolSchemaBytes {
			return fmt.Errorf("%w: response schema too large", iface.ErrLLMInvalidRequest)
		}
	}
	if t := req.Options.Temperature; t != nil && (math.IsNaN(*t) || *t < 0 || *t > 2) {
		return fmt.Errorf("%w: temperature", iface.ErrLLMInvalidRequest)
	}
	if req.Options.MaxOutputTokens != nil && *req.Options.MaxOutputTokens <= 0 {
		return fmt.Errorf("%w: maxOutputTokens", iface.ErrLLMInvalidRequest)
	}
	switch req.Options.Effort {
	case "", "low", "medium", "high":
	default:
		return fmt.Errorf("%w: effort", iface.ErrLLMInvalidRequest)
	}
	return nil
}

// ValidateEmbedRequest enforces the contract limits before any resolution.
func ValidateEmbedRequest(req iface.EmbedRequest) error {
	if !keyRE.MatchString(req.Caller) {
		return fmt.Errorf("%w: caller", iface.ErrLLMInvalidRequest)
	}
	if req.Purpose != "" && !keyRE.MatchString(req.Purpose) {
		return fmt.Errorf("%w: purpose", iface.ErrLLMInvalidRequest)
	}
	if len(req.Inputs) == 0 || len(req.Inputs) > maxEmbedInputs {
		return fmt.Errorf("%w: inputs count", iface.ErrLLMInvalidRequest)
	}
	for _, in := range req.Inputs {
		if in == "" || len(in) > maxEmbedInputBytes {
			return fmt.Errorf("%w: input size", iface.ErrLLMInvalidRequest)
		}
	}
	return nil
}

func needFor(req iface.ChatRequest) Need {
	return Need{Chat: true, Tools: len(req.Tools) > 0, StructuredOutput: req.ResponseFormat != nil}
}

// candidates refuses non-internal tenants, resolves the acting user and the
// ordered models they may use for purpose, and applies an optional pin. A
// pin must be one of the candidates: pinning never widens access.
func (g *Gateway) candidates(ctx context.Context, purpose, pin string, need Need) ([]models.Model, string, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, "", err
	}
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	cands, err := g.access.Candidates(ctx, userUUID, purpose, need)
	if err != nil {
		return nil, "", err
	}
	if pin != "" {
		for _, c := range cands {
			if c.UUID == pin {
				return []models.Model{c}, userUUID, nil
			}
		}
		return nil, "", iface.ErrLLMModelAccessDenied
	}
	if len(cands) == 0 {
		return nil, "", iface.ErrLLMNoEligibleModel
	}
	return cands, userUUID, nil
}

// unavailable: access and validation passed, but no router is wired
// (module not initialized, or a test without one).
func (g *Gateway) unavailable(first models.Model) error {
	if len(g.registry.Kinds()) == 0 {
		return fmt.Errorf("%w: no provider adapters registered for %s", iface.ErrLLMProviderUnavailable, first.Provider)
	}
	return fmt.Errorf("%w: routing not wired", iface.ErrLLMProviderUnavailable)
}

func (g *Gateway) Chat(ctx context.Context, req iface.ChatRequest, sink iface.ChatSink) (*iface.ChatResult, error) {
	if err := ValidateChatRequest(req); err != nil {
		return nil, err
	}
	cands, userUUID, err := g.candidates(ctx, req.EffectivePurpose(), req.ModelUUID, needFor(req))
	if err != nil {
		return nil, err
	}
	if g.router == nil {
		return nil, g.unavailable(cands[0])
	}
	return g.router.Chat(ctx, cands, userUUID, req, sink)
}

func (g *Gateway) Embed(ctx context.Context, req iface.EmbedRequest) (*iface.EmbedResult, error) {
	if err := ValidateEmbedRequest(req); err != nil {
		return nil, err
	}
	cands, userUUID, err := g.candidates(ctx, req.EffectivePurpose(), "", Need{Embeddings: true})
	if err != nil {
		return nil, err
	}
	if g.router == nil {
		return nil, g.unavailable(cands[0])
	}
	return g.router.Embed(ctx, cands, userUUID, req)
}

// Resolve returns the model Chat would pick for purpose, without calling it.
func (g *Gateway) Resolve(ctx context.Context, purpose string) (*iface.LLMModelInfo, error) {
	if purpose != "" && !keyRE.MatchString(purpose) {
		return nil, fmt.Errorf("%w: purpose", iface.ErrLLMInvalidRequest)
	}
	cands, _, err := g.candidates(ctx, purpose, "", Need{Chat: true})
	if err != nil {
		return nil, err
	}
	info := cands[0].Info()
	return &info, nil
}

// ListUsable lists every model the acting user may use in the current org.
func (g *Gateway) ListUsable(ctx context.Context) ([]iface.LLMModelInfo, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	ms, err := g.access.Usable(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	out := make([]iface.LLMModelInfo, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Info())
	}
	return out, nil
}

// chatPinned runs req on exactly m for an admin operation (the prompt
// test): validation and the liveness rules apply as on the live path —
// active model, allow_hosted, no mock in production-like envs, an active
// org credential (ruling 5.3) — but the access resolver, and so the
// grants, is skipped. The call is reserved, routed and ledgered by the
// router like any other, under the acting admin's user UUID. Unexported on
// purpose: the Gateway is the object registered as ServiceLLMGateway, and a
// grant bypass must not be reachable from it; NewPinnedRunner hands it to
// AdminOps only.
func (g *Gateway) chatPinned(ctx context.Context, m models.Model, req iface.ChatRequest, sink iface.ChatSink) (*iface.ChatResult, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	if err := ValidateChatRequest(req); err != nil {
		return nil, err
	}
	if err := g.catalog.pinnable(ctx, m); err != nil {
		return nil, err
	}
	if g.router == nil {
		return nil, g.unavailable(m)
	}
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	return g.router.Chat(ctx, []models.Model{m}, userUUID, req, sink)
}

// pinnedRunner is the grant-bypassing chat path, a separate object from
// the registered Gateway.
type pinnedRunner struct{ g *Gateway }

func (p pinnedRunner) ChatPinned(ctx context.Context, m models.Model, req iface.ChatRequest, sink iface.ChatSink) (*iface.ChatResult, error) {
	return p.g.chatPinned(ctx, m, req, sink)
}

// NewPinnedRunner returns g's admin prompt-test path (grants skipped) for
// AdminOps; the module wires it there and nowhere else.
func NewPinnedRunner(g *Gateway) PromptRunner { return pinnedRunner{g: g} }
