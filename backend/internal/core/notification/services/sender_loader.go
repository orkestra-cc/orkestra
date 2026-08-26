package services

import (
	"context"
	"fmt"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// SnapshotGetter returns the module's configuration document — ONE read.
// module.go wires it to ModuleConfigService.GetConfig.
type SnapshotGetter func(ctx context.Context) (*module.ModuleConfig, error)

// legacySettings are the flat email.* keys, decoded by the SDK against the
// document's own schema so value → env var → default resolution matches
// GetValue/GetSecret exactly — from the snapshot, not from further reads.
type legacySettings struct {
	Provider    string `module:"email.provider"`
	Host        string `module:"email.smtp.host"`
	Port        int    `module:"email.smtp.port"`
	Username    string `module:"email.smtp.username"`
	Password    string `module:"email.smtp.password"`
	FromAddress string `module:"email.from_address"`
	FromName    string `module:"email.from_name"`
	ReplyTo     string `module:"email.reply_to"`
	TLSMode     string `module:"email.smtp.tls_mode"`
}

// NewSnapshotLoader builds the SenderConfigLoader every send calls. Values
// and secrets are decoded from the active environment of ONE document, so
// an environment activation between reads can never pair one environment's
// host with the other's password (D4). A read or decode failure is reported
// as cfg.Err and the resolver fails closed on it.
func NewSnapshotLoader(get SnapshotGetter) SenderConfigLoader {
	return func(ctx context.Context) SenderConfig {
		if get == nil {
			return SenderConfig{Legacy: LegacyProfile(SenderProfile{})}
		}
		doc, err := get(ctx)
		if err != nil {
			return SenderConfig{Err: fmt.Errorf("notification: read config snapshot: %w", err)}
		}
		if doc == nil {
			return SenderConfig{Legacy: LegacyProfile(SenderProfile{})}
		}
		legacy, err := legacyFromSnapshot(doc)
		if err != nil {
			return SenderConfig{Err: err}
		}
		return SenderConfig{Legacy: legacy}
	}
}

func legacyFromSnapshot(doc *module.ModuleConfig) (SenderProfile, error) {
	var ls legacySettings
	if err := module.UnmarshalConfig(doc.ConfigSchema, doc.ActiveConfigValues(), doc.ActiveEncryptedValues(), &ls); err != nil {
		return SenderProfile{}, fmt.Errorf("notification: decode legacy email settings: %w", err)
	}
	if ls.Port == 0 {
		ls.Port = 587
	}
	return LegacyProfile(SenderProfile{
		Provider: ls.Provider, SMTPHost: ls.Host, SMTPPort: ls.Port, SMTPUsername: ls.Username, SMTPPassword: ls.Password,
		FromAddress: ls.FromAddress, FromName: ls.FromName, ReplyTo: ls.ReplyTo, SMTPTLSMode: ls.TLSMode,
	}), nil
}
