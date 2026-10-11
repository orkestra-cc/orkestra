package llm

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var _ module.HasConfigSnapshotValidator = (*Module)(nil)

// siwcMode normalizes a stored mode: absent means disabled.
func siwcMode(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		return services.SIWCModeDisabled
	}
	return v
}

// ValidateConfigSnapshot refuses a contradictory or unsupported Sign in with
// ChatGPT configuration on every mutation surface (active PATCH, named
// environment PATCH, activation). It judges EffectiveValues, so a mode
// supplied by LLM_SIWC_MODE is validated like a stored one.
func (m *Module) ValidateConfigSnapshot(_ context.Context, snap module.ConfigValidationSnapshot) error {
	eff := snap.EffectiveValues
	cfg := services.SIWCConfig{
		Mode:              siwcMode(eff[services.SIWCFieldMode]),
		PastePort:         services.SIWCDefaultPastePort,
		OperatorPublicURL: m.operatorPublicURL,
		ProductionLike:    m.prodLike,
	}
	if cfg.Mode == services.SIWCModeDisabled {
		return nil
	}
	if raw := strings.TrimSpace(eff[services.SIWCFieldPastePort]); raw != "" && cfg.Mode == services.SIWCModeHostedPaste {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return siwcValidationError(services.SIWCFieldPastePort, "siwc_paste_port must be a whole number between 1024 and 65535")
		}
		cfg.PastePort = n
	}
	if raw := strings.TrimSpace(eff[services.SIWCFieldAllowHost]); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return siwcValidationError(services.SIWCFieldAllowHost, "allow_hosted must be true or false")
		}
		cfg.AllowHosted = b
	}
	if err := services.ValidateSIWCConfig(cfg); err != nil {
		var ce *services.SIWCConfigError
		if errors.As(err, &ce) {
			return siwcValidationError(ce.Field, ce.Reason)
		}
		return err
	}
	return nil
}

func siwcValidationError(field, message string) error {
	return &module.ConfigValidationError{Field: field, Message: message, Code: errcode.LLMSIWCConfigInvalid}
}
