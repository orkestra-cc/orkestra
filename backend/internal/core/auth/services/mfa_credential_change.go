package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	notifModels "github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Credential-change plumbing shared by mfaService (TOTP) and
// webAuthnService (passkeys). Spec §4.2 D13, §4.3 D16.
//
// One rule covers every credential removal or replacement — TOTP removal,
// TOTP replacement, any passkey removal, admin reset: bump the MFA epoch,
// revoke device trust, end every session but the caller's own, and announce
// it. The session half lives on the handlers (they alone can see the
// caller's session id); the three service-level halves live here so the two
// services cannot drift apart on them.

// bumpMFAEpoch is the single place either service moves the counter. It is
// called by every REMOVAL or REPLACEMENT and by no addition: authority
// proven by a factor that still exists stays valid.
//
// The seam is OPTIONAL and degrades rather than failing. A fork's user
// provider may predate iface.MFAEpochBumper, in which case the epoch never
// moves and the platform falls back to session revocation alone — exactly
// what it had before the epoch existed. A bump that errors is the same
// contract: the credential is already gone, and failing the caller here
// would tell them a completed removal did not happen.
func bumpMFAEpoch(ctx context.Context, bumper iface.MFAEpochBumper, logger *slog.Logger, userUUID string) {
	if logger == nil {
		logger = slog.Default()
	}
	if bumper == nil {
		logger.Warn("mfa: epoch bumper not wired; the MFA authority of tokens already issued survives until they expire",
			slog.String("user_uuid", userUUID))
		return
	}
	if _, err := bumper.BumpMFAEpoch(ctx, userUUID); err != nil {
		logger.Error("mfa: failed to bump the MFA epoch; issued tokens keep their MFA markers until they expire",
			slog.String("user_uuid", userUUID),
			slog.String("error", err.Error()))
	}
}

// emitCredentialEvent records one credential-change security event. Mirrors
// the RegenerateBackupCodes lane: the persistent sink when it is wired, a
// slog breadcrumb otherwise, so a minimal build still leaves a trace.
func emitCredentialEvent(ctx context.Context, sink SecurityEventSink, logger *slog.Logger, eventType, userUUID string, fields map[string]interface{}) {
	if sink != nil {
		sink.RecordSelfAuthEvent(ctx, eventType, userUUID, fields)
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("self_auth_action",
		"event", eventType,
		"userUUID", userUUID,
	)
}

// MailEnqueuer is the one-method slice of *MailDispatcher the factor-added
// notifier consumes. It exists so the notifier depends on the behaviour
// ("hand this send to the bounded pool") rather than on the concrete
// dispatcher, which a test cannot observe.
type MailEnqueuer interface {
	Enqueue(job MailJob) bool
}

// FactorOwnerLookup is the single method the notifier needs from the tier's
// user provider: the address to mail. iface.UserProvider satisfies it
// structurally, so the wiring hands over the tier provider unchanged
// without this package taking a dependency on the whole surface.
type FactorOwnerLookup interface {
	GetUserByID(ctx context.Context, id string) (*iface.User, error)
}

// FactorAddedNotifier emails the account holder whenever a second factor is
// added to their account — a first TOTP enrolment, a TOTP replacement, or a
// new passkey.
//
// This is the notice that makes a stolen-bearer enrolment visible: before
// it, only backup-code regeneration emitted anything at all, so an attacker
// who enrolled their own authenticator did so silently. Removals are NOT
// announced here — they already end every other session, which is louder
// than an email.
//
// The send goes through the bounded dispatcher, never inline: a slow SMTP
// relay must not add latency to the enrolment request.
type FactorAddedNotifier struct {
	notifier     iface.NotificationSender
	mail         MailEnqueuer
	users        FactorOwnerLookup
	appName      string
	supportEmail string
	logger       *slog.Logger
}

// NewFactorAddedNotifier builds the notifier. Every collaborator is
// tolerated as nil — a deployment without a notification module, without a
// mail dispatcher, or with neither still enrols factors; it just does so
// silently.
func NewFactorAddedNotifier(
	notifier iface.NotificationSender,
	mail MailEnqueuer,
	users FactorOwnerLookup,
	appName, supportEmail string,
	logger *slog.Logger,
) *FactorAddedNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	if appName == "" {
		appName = "Orkestra"
	}
	return &FactorAddedNotifier{
		notifier:     notifier,
		mail:         mail,
		users:        users,
		appName:      appName,
		supportEmail: supportEmail,
		logger:       logger,
	}
}

// NotifyFactorAdded enqueues the auth.mfa_factor_added mail. factorType is
// "totp" or "passkey"; replaced says whether this addition also removed an
// earlier secret, which changes the copy from "a factor was added" to "your
// authenticator was replaced".
//
// Nil-receiver tolerant so callers can hold an unset seam without guarding
// every call site.
func (n *FactorAddedNotifier) NotifyFactorAdded(ctx context.Context, userUUID, factorType string, replaced bool) {
	if n == nil || n.users == nil || n.mail == nil || n.notifier == nil {
		return
	}
	if !iface.IsConfiguredForCategory(ctx, n.notifier, notifModels.CategoryAuthSecurity) {
		return
	}
	user, err := n.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil || user.Email == "" {
		n.logger.Warn("auth: factor-added email skipped; recipient could not be resolved",
			slog.String("user_uuid", userUUID))
		return
	}
	userName := user.FullName
	if userName == "" {
		userName = user.Email
	}
	at := time.Now().UTC()
	ip, _ := ipFromCtx(ctx)
	req := iface.TemplatedNotificationRequest{
		Channel:    notifModels.ChannelEmail,
		Type:       notifModels.TypeTransactional,
		Category:   notifModels.CategoryAuthSecurity,
		TemplateID: notifModels.TemplateAuthMFAFactorAdded,
		Recipients: []iface.Recipient{{
			UserUUID: user.UUID,
			Address:  user.Email,
			Name:     userName,
		}},
		Data: map[string]any{
			"AppName":      n.appName,
			"UserName":     userName,
			"FactorType":   factorType,
			"Replaced":     replaced,
			"RequestIP":    ip,
			"At":           at.Format("2006-01-02 15:04 UTC"),
			"SupportEmail": n.supportEmail,
		},
		IdempotencyKey: fmt.Sprintf("mfa-factor-added:%s:%s:%d", user.UUID, factorType, at.Unix()),
	}
	notifier := n.notifier
	n.mail.Enqueue(MailJob{
		TemplateID: notifModels.TemplateAuthMFAFactorAdded,
		Send: func(sendCtx context.Context) error {
			_, err := notifier.SendTemplated(sendCtx, req)
			return err
		},
	})
}
