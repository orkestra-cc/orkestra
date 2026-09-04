package services

import "github.com/orkestra/backend/internal/core/notification/models"

// defaultTemplate bundles a TemplateDoc payload for seeding.
type defaultTemplate struct {
	TemplateID  string
	Locale      string
	Subject     string
	BodyText    string
	BodyHTML    string
	Description string
	Variables   []string
}

// defaultTemplates are seeded into the DB on module Start() if missing.
// They are plain text/template strings — Go's text/template + html/template
// pipelines render them at dispatch time.
var defaultTemplates = []defaultTemplate{
	{
		TemplateID:  models.CategoryAuthVerifyEmail,
		Locale:      "en",
		Subject:     "Verify your {{.AppName}} email",
		Description: "Sent on signup to confirm the user's email address.",
		Variables:   []string{"AppName", "UserName", "VerifyURL", "ExpiresIn", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Hi {{.UserName}},

Welcome to {{.AppName}}. Please verify your email address by visiting the link below:

{{.VerifyURL}}

This link expires in {{.ExpiresIn}}.

If you did not create an account, you can safely ignore this email.

Need help? Contact {{.SupportEmail}}.

— The {{.AppName}} team

---
You received this email because someone created an account with this address.
Manage preferences: {{.PreferencesURL}}
Unsubscribe from marketing: {{.UnsubscribeURL}}
You will still receive security-related emails.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Verify your email</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">Welcome to {{.AppName}}</h2>
  <p>Hi {{.UserName}},</p>
  <p>Please confirm your email address to finish setting up your account.</p>
  <p style="margin:32px 0;">
    <a href="{{.VerifyURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Verify email</a>
  </p>
  <p style="color:#6c757d;font-size:14px;">This link expires in {{.ExpiresIn}}.</p>
  <p style="color:#6c757d;font-size:14px;">If the button doesn't work, paste this URL in your browser:<br><span style="word-break:break-all;">{{.VerifyURL}}</span></p>
  <p style="color:#6c757d;font-size:14px;">If you did not create an account, you can safely ignore this email.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">You received this email because someone created an account with this address.<br>
  <a href="{{.PreferencesURL}}" style="color:#9ca3af;">Manage preferences</a> &middot;
  <a href="{{.UnsubscribeURL}}" style="color:#9ca3af;">Unsubscribe from marketing</a><br>
  You will still receive security-related emails.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthSuspiciousLogin,
		Locale:      "en",
		Subject:     "Suspicious login on your {{.AppName}} account",
		Description: "Sent when the risk scorer flags a login at or above the high bucket (>= 0.5).",
		Variables:   []string{"AppName", "UserName", "LoginAt", "LoginIP", "LoginDevice", "LoginLocation", "RiskLevel", "RiskFactors", "AccountActivityURL", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Hi {{.UserName}},

We detected a sign-in to your {{.AppName}} account that looked unusual.

When:    {{.LoginAt}}
From:    {{.LoginIP}}{{if .LoginLocation}} ({{.LoginLocation}}){{end}}
Device:  {{.LoginDevice}}
Risk:    {{.RiskLevel}}{{if .RiskFactors}} — {{.RiskFactors}}{{end}}

If this was you, no action is needed.

If you do NOT recognize this sign-in:
  1. Change your password immediately at {{.AccountActivityURL}}
  2. Review recent activity and sign out of any device you don't recognize
  3. Enable or verify multi-factor authentication

Review recent account activity: {{.AccountActivityURL}}

Need help? Contact {{.SupportEmail}}.

— The {{.AppName}} security team

---
Manage preferences: {{.PreferencesURL}}
You will still receive security-related emails.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Suspicious login</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#b91c1c;">Suspicious login detected</h2>
  <p>Hi {{.UserName}},</p>
  <p>We detected a sign-in to your {{.AppName}} account that looked unusual. Review the details below.</p>
  <table cellpadding="6" style="border-collapse:collapse;margin:16px 0;font-size:14px;">
    <tr><td style="color:#6c757d;">When</td><td><strong>{{.LoginAt}}</strong></td></tr>
    <tr><td style="color:#6c757d;">From</td><td><code>{{.LoginIP}}</code>{{if .LoginLocation}} <span style="color:#6c757d;">({{.LoginLocation}})</span>{{end}}</td></tr>
    <tr><td style="color:#6c757d;">Device</td><td>{{.LoginDevice}}</td></tr>
    <tr><td style="color:#6c757d;">Risk</td><td><strong>{{.RiskLevel}}</strong>{{if .RiskFactors}} <span style="color:#6c757d;">— {{.RiskFactors}}</span>{{end}}</td></tr>
  </table>
  <p style="margin:24px 0;">
    <a href="{{.AccountActivityURL}}" style="background:#b91c1c;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Review account activity</a>
  </p>
  <p>If this was you, no action is needed. If you do not recognize this sign-in:</p>
  <ol style="color:#333;">
    <li>Change your password immediately.</li>
    <li>Review recent activity and sign out of any device you don't recognize.</li>
    <li>Enable or verify multi-factor authentication.</li>
  </ol>
  <p style="color:#6c757d;font-size:14px;">Need help? Contact <a href="mailto:{{.SupportEmail}}" style="color:#6c757d;">{{.SupportEmail}}</a>.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">
    <a href="{{.PreferencesURL}}" style="color:#9ca3af;">Manage preferences</a><br>
    You will still receive security-related emails.
  </p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthResetPassword,
		Locale:      "en",
		Subject:     "Reset your {{.AppName}} password",
		Description: "Sent when the user requests a password reset.",
		Variables:   []string{"AppName", "UserName", "ResetURL", "ExpiresIn", "SupportEmail", "RequestIP", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Hi {{.UserName}},

We received a request to reset your {{.AppName}} password. Use the link below within {{.ExpiresIn}} to pick a new one:

{{.ResetURL}}

If you did not request a password reset, ignore this email and your password will remain unchanged. You may want to review your account activity.

Requested from IP: {{.RequestIP}}

Need help? Contact {{.SupportEmail}}.

— The {{.AppName}} team

---
Manage preferences: {{.PreferencesURL}}
You will still receive security-related emails.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Reset your password</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">Reset your password</h2>
  <p>Hi {{.UserName}},</p>
  <p>We received a request to reset your {{.AppName}} password. Click the button below to pick a new one.</p>
  <p style="margin:32px 0;">
    <a href="{{.ResetURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Reset password</a>
  </p>
  <p style="color:#6c757d;font-size:14px;">This link expires in {{.ExpiresIn}}.</p>
  <p style="color:#6c757d;font-size:14px;">If the button doesn't work, paste this URL in your browser:<br><span style="word-break:break-all;">{{.ResetURL}}</span></p>
  <p style="color:#6c757d;font-size:14px;">If you did not request a password reset, ignore this email and your password will remain unchanged. You may want to review your account activity.</p>
  <p style="color:#6c757d;font-size:14px;">Requested from IP: <code>{{.RequestIP}}</code></p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;"><a href="{{.PreferencesURL}}" style="color:#9ca3af;">Manage preferences</a><br>You will still receive security-related emails.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthNewDeviceLogin,
		Locale:      "en",
		Subject:     "New sign-in to your {{.AppName}} account",
		Description: "Sent the first time a user signs in from a (deviceId, userUUID) pair the system has not seen before.",
		Variables:   []string{"AppName", "UserName", "LoginAt", "LoginIP", "LoginDevice", "LoginLocation", "AccountActivityURL", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Hi {{.UserName}},

A new device just signed in to your {{.AppName}} account.

When:    {{.LoginAt}}
From:    {{.LoginIP}}{{if .LoginLocation}} ({{.LoginLocation}}){{end}}
Device:  {{.LoginDevice}}

If this was you, no action is needed.

If you do NOT recognize this sign-in, change your password and review recent activity at {{.AccountActivityURL}}.

Need help? Contact {{.SupportEmail}}.

— The {{.AppName}} security team

---
Manage preferences: {{.PreferencesURL}}
You will still receive security-related emails.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>New device sign-in</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">New device sign-in</h2>
  <p>Hi {{.UserName}},</p>
  <p>A new device just signed in to your {{.AppName}} account.</p>
  <table cellpadding="6" style="border-collapse:collapse;margin:16px 0;font-size:14px;">
    <tr><td style="color:#6c757d;">When</td><td><strong>{{.LoginAt}}</strong></td></tr>
    <tr><td style="color:#6c757d;">From</td><td><code>{{.LoginIP}}</code>{{if .LoginLocation}} <span style="color:#6c757d;">({{.LoginLocation}})</span>{{end}}</td></tr>
    <tr><td style="color:#6c757d;">Device</td><td>{{.LoginDevice}}</td></tr>
  </table>
  <p style="margin:24px 0;">
    <a href="{{.AccountActivityURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Review account activity</a>
  </p>
  <p>If this was you, no action is needed. If you do not recognize this sign-in, change your password and sign out of any device you don't recognize.</p>
  <p style="color:#6c757d;font-size:14px;">Need help? Contact <a href="mailto:{{.SupportEmail}}" style="color:#6c757d;">{{.SupportEmail}}</a>.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;"><a href="{{.PreferencesURL}}" style="color:#9ca3af;">Manage preferences</a><br>You will still receive security-related emails.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthAdminSuspiciousLogin,
		Locale:      "en",
		Subject:     "[{{.AppName}}] Suspicious login: {{.AffectedUserEmail}}",
		Description: "Admin-side notification when a user's login is flagged high-risk. Gated by notifyAdminOnSuspiciousLogin + suspiciousLoginRecipients.",
		Variables:   []string{"AppName", "AffectedUserName", "AffectedUserEmail", "AffectedUserUUID", "LoginAt", "LoginIP", "LoginDevice", "LoginLocation", "RiskLevel", "RiskFactors", "AccountActivityURL", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Suspicious login alert.

User:    {{.AffectedUserName}} <{{.AffectedUserEmail}}> (uuid {{.AffectedUserUUID}})
When:    {{.LoginAt}}
From:    {{.LoginIP}}{{if .LoginLocation}} ({{.LoginLocation}}){{end}}
Device:  {{.LoginDevice}}
Risk:    {{.RiskLevel}}{{if .RiskFactors}} — {{.RiskFactors}}{{end}}

The user has been notified. Review activity: {{.AccountActivityURL}}

— {{.AppName}} security alerting`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Admin: suspicious login</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#b91c1c;">Suspicious login alert</h2>
  <p>A login on <strong>{{.AppName}}</strong> was flagged as high-risk. The affected user has already been notified.</p>
  <table cellpadding="6" style="border-collapse:collapse;margin:16px 0;font-size:14px;">
    <tr><td style="color:#6c757d;">User</td><td>{{.AffectedUserName}} &lt;{{.AffectedUserEmail}}&gt;<br><code style="color:#6c757d;">{{.AffectedUserUUID}}</code></td></tr>
    <tr><td style="color:#6c757d;">When</td><td><strong>{{.LoginAt}}</strong></td></tr>
    <tr><td style="color:#6c757d;">From</td><td><code>{{.LoginIP}}</code>{{if .LoginLocation}} <span style="color:#6c757d;">({{.LoginLocation}})</span>{{end}}</td></tr>
    <tr><td style="color:#6c757d;">Device</td><td>{{.LoginDevice}}</td></tr>
    <tr><td style="color:#6c757d;">Risk</td><td><strong>{{.RiskLevel}}</strong>{{if .RiskFactors}} <span style="color:#6c757d;">— {{.RiskFactors}}</span>{{end}}</td></tr>
  </table>
  <p style="margin:24px 0;">
    <a href="{{.AccountActivityURL}}" style="background:#b91c1c;color:#fff;padding:10px 18px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Review activity</a>
  </p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">Sent because notifyAdminOnSuspiciousLogin is enabled.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthAdminInvite,
		Locale:      "en",
		Subject:     "You've been invited to {{.AppName}}",
		Description: "Sent when an admin operator invites a new Tier-2 client user. The recipient redeems the token on the client SPA's /accept-invite page; redemption sets their password and marks the email verified.",
		Variables:   []string{"AppName", "UserName", "InviteURL", "ExpiresIn", "InviterName", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Hi {{.UserName}},

{{if .InviterName}}{{.InviterName}} has invited you{{else}}You've been invited{{end}} to join {{.AppName}}.

Use the link below within {{.ExpiresIn}} to set your password and finish setting up your account:

{{.InviteURL}}

If you weren't expecting this invitation you can safely ignore this email.

Need help? Contact {{.SupportEmail}}.

— The {{.AppName}} team

---
Manage preferences: {{.PreferencesURL}}
You will still receive security-related emails.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>You've been invited</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">You've been invited to {{.AppName}}</h2>
  <p>Hi {{.UserName}},</p>
  <p>{{if .InviterName}}<strong>{{.InviterName}}</strong> has invited you{{else}}You've been invited{{end}} to join {{.AppName}}. Use the button below to set your password and finish setting up your account.</p>
  <p style="margin:32px 0;">
    <a href="{{.InviteURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Accept invitation</a>
  </p>
  <p style="color:#6c757d;font-size:14px;">This link expires in {{.ExpiresIn}}.</p>
  <p style="color:#6c757d;font-size:14px;">If the button doesn't work, paste this URL in your browser:<br><span style="word-break:break-all;">{{.InviteURL}}</span></p>
  <p style="color:#6c757d;font-size:14px;">If you weren't expecting this invitation, you can safely ignore this email.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;"><a href="{{.PreferencesURL}}" style="color:#9ca3af;">Manage preferences</a><br>You will still receive security-related emails.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.TemplateAuthMFAFactorAdded,
		Locale:      "en",
		Subject:     "A second factor was added to your {{.AppName}} account",
		Description: "Sent when a second factor is added to an account — a first TOTP enrolment, a TOTP replacement, or a new passkey. It is what makes an enrolment performed with a stolen session visible to the account holder.",
		Variables:   []string{"AppName", "UserName", "FactorType", "Replaced", "RequestIP", "At", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Hi {{.UserName}},

{{if .Replaced}}The authenticator app on your {{.AppName}} account was replaced.{{else}}A new second factor was added to your {{.AppName}} account.{{end}}

Type:  {{if eq .FactorType "passkey"}}Passkey{{else}}Authenticator app (TOTP){{end}}
When:  {{.At}}
From:  {{.RequestIP}}

If this was you, no action is needed.{{if .Replaced}} Your previous authenticator has stopped working, and every other signed-in session was signed out.{{end}}

If it was NOT you, someone else has access to your account: change your password now and contact {{.SupportEmail}}.

— The {{.AppName}} team

---
This is a security notification and cannot be turned off.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>A second factor was added</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">{{if .Replaced}}Your authenticator was replaced{{else}}A second factor was added{{end}}</h2>
  <p>Hi {{.UserName}},</p>
  <p>{{if .Replaced}}The authenticator app on your {{.AppName}} account was replaced.{{else}}A new second factor was added to your {{.AppName}} account.{{end}}</p>
  <table style="border-collapse:collapse;margin:24px 0;font-size:14px;">
    <tr><td style="padding:4px 16px 4px 0;color:#6c757d;">Type</td><td style="padding:4px 0;">{{if eq .FactorType "passkey"}}Passkey{{else}}Authenticator app (TOTP){{end}}</td></tr>
    <tr><td style="padding:4px 16px 4px 0;color:#6c757d;">When</td><td style="padding:4px 0;">{{.At}}</td></tr>
    <tr><td style="padding:4px 16px 4px 0;color:#6c757d;">From</td><td style="padding:4px 0;">{{.RequestIP}}</td></tr>
  </table>
  <p style="color:#6c757d;font-size:14px;">If this was you, no action is needed.{{if .Replaced}} Your previous authenticator has stopped working, and every other signed-in session was signed out.{{end}}</p>
  <p style="color:#b91c1c;font-size:14px;">If it was <strong>not</strong> you, someone else has access to your account: change your password now and contact <a href="mailto:{{.SupportEmail}}" style="color:#b91c1c;">{{.SupportEmail}}</a>.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">This is a security notification and cannot be turned off.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthVerifyEmail,
		Locale:      "it",
		Subject:     "Verifica il suo indirizzo email {{.AppName}}",
		Description: "Sent on signup to confirm the user's email address.",
		Variables:   []string{"AppName", "UserName", "VerifyURL", "ExpiresIn", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Gentile {{.UserName}},

Le diamo il benvenuto su {{.AppName}}. La invitiamo a verificare il suo indirizzo email seguendo il link qui sotto:

{{.VerifyURL}}

Il link scade tra {{.ExpiresIn}}.

Se non ha creato lei questo account, può ignorare questo messaggio in tutta sicurezza.

Serve aiuto? Contatti {{.SupportEmail}}.

— Il team di {{.AppName}}

---
Ha ricevuto questo messaggio perché è stato creato un account con questo indirizzo email.
Gestisci le preferenze: {{.PreferencesURL}}
Annulla l'iscrizione alle comunicazioni di marketing: {{.UnsubscribeURL}}
Continuerà comunque a ricevere le email relative alla sicurezza.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Verifica la sua email</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">Le diamo il benvenuto su {{.AppName}}</h2>
  <p>Gentile {{.UserName}},</p>
  <p>La invitiamo a confermare il suo indirizzo email per completare la configurazione dell'account.</p>
  <p style="margin:32px 0;">
    <a href="{{.VerifyURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Verifica email</a>
  </p>
  <p style="color:#6c757d;font-size:14px;">Il link scade tra {{.ExpiresIn}}.</p>
  <p style="color:#6c757d;font-size:14px;">Se il pulsante non funziona, copi e incolli questo indirizzo nel browser:<br><span style="word-break:break-all;">{{.VerifyURL}}</span></p>
  <p style="color:#6c757d;font-size:14px;">Se non ha creato lei questo account, può ignorare questo messaggio in tutta sicurezza.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">Ha ricevuto questo messaggio perché è stato creato un account con questo indirizzo email.<br>
  <a href="{{.PreferencesURL}}" style="color:#9ca3af;">Gestisci le preferenze</a> &middot;
  <a href="{{.UnsubscribeURL}}" style="color:#9ca3af;">Annulla l'iscrizione alle comunicazioni di marketing</a><br>
  Continuerà comunque a ricevere le email relative alla sicurezza.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthSuspiciousLogin,
		Locale:      "it",
		Subject:     "Accesso sospetto al suo account {{.AppName}}",
		Description: "Sent when the risk scorer flags a login at or above the high bucket (>= 0.5).",
		Variables:   []string{"AppName", "UserName", "LoginAt", "LoginIP", "LoginDevice", "LoginLocation", "RiskLevel", "RiskFactors", "AccountActivityURL", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Gentile {{.UserName}},

Abbiamo rilevato un accesso al suo account {{.AppName}} che ci è sembrato insolito.

Quando:      {{.LoginAt}}
Da:          {{.LoginIP}}{{if .LoginLocation}} ({{.LoginLocation}}){{end}}
Dispositivo: {{.LoginDevice}}
Rischio:     {{.RiskLevel}}{{if .RiskFactors}} — {{.RiskFactors}}{{end}}

Se è stato lei, non è necessaria alcuna azione.

Se NON riconosce questo accesso:
  1. Cambi subito la password su {{.AccountActivityURL}}
  2. Controlli l'attività recente ed esca da ogni dispositivo che non riconosce
  3. Attivi o verifichi l'autenticazione a più fattori

Controlli l'attività recente dell'account: {{.AccountActivityURL}}

Serve aiuto? Contatti {{.SupportEmail}}.

— Il team di sicurezza di {{.AppName}}

---
Gestisci le preferenze: {{.PreferencesURL}}
Continuerà comunque a ricevere le email relative alla sicurezza.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Accesso sospetto</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#b91c1c;">Accesso sospetto rilevato</h2>
  <p>Gentile {{.UserName}},</p>
  <p>Abbiamo rilevato un accesso al suo account {{.AppName}} che ci è sembrato insolito. Controlli i dettagli qui sotto.</p>
  <table cellpadding="6" style="border-collapse:collapse;margin:16px 0;font-size:14px;">
    <tr><td style="color:#6c757d;">Quando</td><td><strong>{{.LoginAt}}</strong></td></tr>
    <tr><td style="color:#6c757d;">Da</td><td><code>{{.LoginIP}}</code>{{if .LoginLocation}} <span style="color:#6c757d;">({{.LoginLocation}})</span>{{end}}</td></tr>
    <tr><td style="color:#6c757d;">Dispositivo</td><td>{{.LoginDevice}}</td></tr>
    <tr><td style="color:#6c757d;">Rischio</td><td><strong>{{.RiskLevel}}</strong>{{if .RiskFactors}} <span style="color:#6c757d;">— {{.RiskFactors}}</span>{{end}}</td></tr>
  </table>
  <p style="margin:24px 0;">
    <a href="{{.AccountActivityURL}}" style="background:#b91c1c;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Controlla l'attività dell'account</a>
  </p>
  <p>Se è stato lei, non è necessaria alcuna azione. Se non riconosce questo accesso:</p>
  <ol style="color:#333;">
    <li>Cambi subito la password.</li>
    <li>Controlli l'attività recente ed esca da ogni dispositivo che non riconosce.</li>
    <li>Attivi o verifichi l'autenticazione a più fattori.</li>
  </ol>
  <p style="color:#6c757d;font-size:14px;">Serve aiuto? Contatti <a href="mailto:{{.SupportEmail}}" style="color:#6c757d;">{{.SupportEmail}}</a>.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">
    <a href="{{.PreferencesURL}}" style="color:#9ca3af;">Gestisci le preferenze</a><br>
    Continuerà comunque a ricevere le email relative alla sicurezza.
  </p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthResetPassword,
		Locale:      "it",
		Subject:     "Reimposta la sua password {{.AppName}}",
		Description: "Sent when the user requests a password reset.",
		Variables:   []string{"AppName", "UserName", "ResetURL", "ExpiresIn", "SupportEmail", "RequestIP", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Gentile {{.UserName}},

Abbiamo ricevuto una richiesta di reimpostazione della sua password {{.AppName}}. Utilizzi il link sottostante entro {{.ExpiresIn}} per sceglierne una nuova:

{{.ResetURL}}

Se non ha richiesto lei la reimpostazione della password, ignori questo messaggio: la sua password resterà invariata. Le consigliamo comunque di controllare l'attività recente del suo account.

Richiesta effettuata dall'indirizzo IP: {{.RequestIP}}

Serve aiuto? Contatti {{.SupportEmail}}.

— Il team di {{.AppName}}

---
Gestisci le preferenze: {{.PreferencesURL}}
Continuerà comunque a ricevere le email relative alla sicurezza.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Reimposta la sua password</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">Reimposta la password</h2>
  <p>Gentile {{.UserName}},</p>
  <p>Abbiamo ricevuto una richiesta di reimpostazione della sua password {{.AppName}}. Clicchi sul pulsante sottostante per sceglierne una nuova.</p>
  <p style="margin:32px 0;">
    <a href="{{.ResetURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Reimposta password</a>
  </p>
  <p style="color:#6c757d;font-size:14px;">Il link scade tra {{.ExpiresIn}}.</p>
  <p style="color:#6c757d;font-size:14px;">Se il pulsante non funziona, copi e incolli questo indirizzo nel browser:<br><span style="word-break:break-all;">{{.ResetURL}}</span></p>
  <p style="color:#6c757d;font-size:14px;">Se non ha richiesto lei la reimpostazione della password, ignori questo messaggio: la sua password resterà invariata. Le consigliamo comunque di controllare l'attività recente del suo account.</p>
  <p style="color:#6c757d;font-size:14px;">Richiesta effettuata dall'indirizzo IP: <code>{{.RequestIP}}</code></p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;"><a href="{{.PreferencesURL}}" style="color:#9ca3af;">Gestisci le preferenze</a><br>Continuerà comunque a ricevere le email relative alla sicurezza.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthNewDeviceLogin,
		Locale:      "it",
		Subject:     "Nuovo accesso al suo account {{.AppName}}",
		Description: "Sent the first time a user signs in from a (deviceId, userUUID) pair the system has not seen before.",
		Variables:   []string{"AppName", "UserName", "LoginAt", "LoginIP", "LoginDevice", "LoginLocation", "AccountActivityURL", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Gentile {{.UserName}},

Un nuovo dispositivo ha appena effettuato l'accesso al suo account {{.AppName}}.

Quando:      {{.LoginAt}}
Da:          {{.LoginIP}}{{if .LoginLocation}} ({{.LoginLocation}}){{end}}
Dispositivo: {{.LoginDevice}}

Se è stato lei, non è necessaria alcuna azione.

Se NON riconosce questo accesso, cambi la password e controlli l'attività recente su {{.AccountActivityURL}}.

Serve aiuto? Contatti {{.SupportEmail}}.

— Il team di sicurezza di {{.AppName}}

---
Gestisci le preferenze: {{.PreferencesURL}}
Continuerà comunque a ricevere le email relative alla sicurezza.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Nuovo accesso da dispositivo</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">Nuovo accesso da dispositivo</h2>
  <p>Gentile {{.UserName}},</p>
  <p>Un nuovo dispositivo ha appena effettuato l'accesso al suo account {{.AppName}}.</p>
  <table cellpadding="6" style="border-collapse:collapse;margin:16px 0;font-size:14px;">
    <tr><td style="color:#6c757d;">Quando</td><td><strong>{{.LoginAt}}</strong></td></tr>
    <tr><td style="color:#6c757d;">Da</td><td><code>{{.LoginIP}}</code>{{if .LoginLocation}} <span style="color:#6c757d;">({{.LoginLocation}})</span>{{end}}</td></tr>
    <tr><td style="color:#6c757d;">Dispositivo</td><td>{{.LoginDevice}}</td></tr>
  </table>
  <p style="margin:24px 0;">
    <a href="{{.AccountActivityURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Controlla l'attività dell'account</a>
  </p>
  <p>Se è stato lei, non è necessaria alcuna azione. Se non riconosce questo accesso, cambi la password ed esca da ogni dispositivo che non riconosce.</p>
  <p style="color:#6c757d;font-size:14px;">Serve aiuto? Contatti <a href="mailto:{{.SupportEmail}}" style="color:#6c757d;">{{.SupportEmail}}</a>.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;"><a href="{{.PreferencesURL}}" style="color:#9ca3af;">Gestisci le preferenze</a><br>Continuerà comunque a ricevere le email relative alla sicurezza.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthAdminSuspiciousLogin,
		Locale:      "it",
		Subject:     "[{{.AppName}}] Accesso sospetto: {{.AffectedUserEmail}}",
		Description: "Admin-side notification when a user's login is flagged high-risk. Gated by notifyAdminOnSuspiciousLogin + suspiciousLoginRecipients.",
		Variables:   []string{"AppName", "AffectedUserName", "AffectedUserEmail", "AffectedUserUUID", "LoginAt", "LoginIP", "LoginDevice", "LoginLocation", "RiskLevel", "RiskFactors", "AccountActivityURL", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Avviso di accesso sospetto.

Utente:      {{.AffectedUserName}} <{{.AffectedUserEmail}}> (uuid {{.AffectedUserUUID}})
Quando:      {{.LoginAt}}
Da:          {{.LoginIP}}{{if .LoginLocation}} ({{.LoginLocation}}){{end}}
Dispositivo: {{.LoginDevice}}
Rischio:     {{.RiskLevel}}{{if .RiskFactors}} — {{.RiskFactors}}{{end}}

L'utente è stato avvisato. Controlla l'attività: {{.AccountActivityURL}}

— Sistema di allerta sicurezza di {{.AppName}}`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Admin: accesso sospetto</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#b91c1c;">Avviso di accesso sospetto</h2>
  <p>Un accesso su <strong>{{.AppName}}</strong> è stato segnalato come ad alto rischio. L'utente interessato è già stato avvisato.</p>
  <table cellpadding="6" style="border-collapse:collapse;margin:16px 0;font-size:14px;">
    <tr><td style="color:#6c757d;">Utente</td><td>{{.AffectedUserName}} &lt;{{.AffectedUserEmail}}&gt;<br><code style="color:#6c757d;">{{.AffectedUserUUID}}</code></td></tr>
    <tr><td style="color:#6c757d;">Quando</td><td><strong>{{.LoginAt}}</strong></td></tr>
    <tr><td style="color:#6c757d;">Da</td><td><code>{{.LoginIP}}</code>{{if .LoginLocation}} <span style="color:#6c757d;">({{.LoginLocation}})</span>{{end}}</td></tr>
    <tr><td style="color:#6c757d;">Dispositivo</td><td>{{.LoginDevice}}</td></tr>
    <tr><td style="color:#6c757d;">Rischio</td><td><strong>{{.RiskLevel}}</strong>{{if .RiskFactors}} <span style="color:#6c757d;">— {{.RiskFactors}}</span>{{end}}</td></tr>
  </table>
  <p style="margin:24px 0;">
    <a href="{{.AccountActivityURL}}" style="background:#b91c1c;color:#fff;padding:10px 18px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Controlla l'attività</a>
  </p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">Inviato perché notifyAdminOnSuspiciousLogin è attivo.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.CategoryAuthAdminInvite,
		Locale:      "it",
		Subject:     "Ha ricevuto un invito a {{.AppName}}",
		Description: "Sent when an admin operator invites a new Tier-2 client user. The recipient redeems the token on the client SPA's /accept-invite page; redemption sets their password and marks the email verified.",
		Variables:   []string{"AppName", "UserName", "InviteURL", "ExpiresIn", "InviterName", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Gentile {{.UserName}},

{{if .InviterName}}{{.InviterName}} le ha inviato un invito{{else}}Le è stato inviato un invito{{end}} per unirsi a {{.AppName}}.

Utilizzi il link sottostante entro {{.ExpiresIn}} per impostare la password e completare la configurazione del suo account:

{{.InviteURL}}

Se non si aspettava questo invito, può ignorare questo messaggio in tutta sicurezza.

Serve aiuto? Contatti {{.SupportEmail}}.

— Il team di {{.AppName}}

---
Gestisci le preferenze: {{.PreferencesURL}}
Continuerà comunque a ricevere le email relative alla sicurezza.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Ha ricevuto un invito</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">Ha ricevuto un invito a {{.AppName}}</h2>
  <p>Gentile {{.UserName}},</p>
  <p>{{if .InviterName}}<strong>{{.InviterName}}</strong> le ha inviato un invito{{else}}Le è stato inviato un invito{{end}} per unirsi a {{.AppName}}. Utilizzi il pulsante sottostante per impostare la password e completare la configurazione del suo account.</p>
  <p style="margin:32px 0;">
    <a href="{{.InviteURL}}" style="background:#2c7be5;color:#fff;padding:12px 24px;text-decoration:none;border-radius:4px;display:inline-block;font-weight:600;">Accetta invito</a>
  </p>
  <p style="color:#6c757d;font-size:14px;">Il link scade tra {{.ExpiresIn}}.</p>
  <p style="color:#6c757d;font-size:14px;">Se il pulsante non funziona, copi e incolli questo indirizzo nel browser:<br><span style="word-break:break-all;">{{.InviteURL}}</span></p>
  <p style="color:#6c757d;font-size:14px;">Se non si aspettava questo invito, può ignorare questo messaggio in tutta sicurezza.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;"><a href="{{.PreferencesURL}}" style="color:#9ca3af;">Gestisci le preferenze</a><br>Continuerà comunque a ricevere le email relative alla sicurezza.</p>
</body>
</html>`,
	},
	{
		TemplateID:  models.TemplateAuthMFAFactorAdded,
		Locale:      "it",
		Subject:     "Un secondo fattore è stato aggiunto al suo account {{.AppName}}",
		Description: "Sent when a second factor is added to an account — a first TOTP enrolment, a TOTP replacement, or a new passkey. It is what makes an enrolment performed with a stolen session visible to the account holder.",
		Variables:   []string{"AppName", "UserName", "FactorType", "Replaced", "RequestIP", "At", "SupportEmail", "UnsubscribeURL", "PreferencesURL"},
		BodyText: `Gentile {{.UserName}},

{{if .Replaced}}L'app di autenticazione del suo account {{.AppName}} è stata sostituita.{{else}}Un nuovo secondo fattore è stato aggiunto al suo account {{.AppName}}.{{end}}

Tipo:  {{if eq .FactorType "passkey"}}Passkey{{else}}App di autenticazione (TOTP){{end}}
Data:  {{.At}}
Da:    {{.RequestIP}}

Se è stato lei, non deve fare nulla.{{if .Replaced}} L'autenticatore precedente non funziona più e tutte le altre sessioni attive sono state disconnesse.{{end}}

Se NON è stato lei, qualcun altro ha accesso al suo account: cambi subito la password e contatti {{.SupportEmail}}.

— Il team di {{.AppName}}

---
Questa è una notifica di sicurezza e non può essere disattivata.`,
		BodyHTML: `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Secondo fattore aggiunto</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:0 auto;padding:32px 24px;color:#333;">
  <h2 style="color:#2c3e50;">{{if .Replaced}}Il suo autenticatore è stato sostituito{{else}}Un secondo fattore è stato aggiunto{{end}}</h2>
  <p>Gentile {{.UserName}},</p>
  <p>{{if .Replaced}}L'app di autenticazione del suo account {{.AppName}} è stata sostituita.{{else}}Un nuovo secondo fattore è stato aggiunto al suo account {{.AppName}}.{{end}}</p>
  <table style="border-collapse:collapse;margin:24px 0;font-size:14px;">
    <tr><td style="padding:4px 16px 4px 0;color:#6c757d;">Tipo</td><td style="padding:4px 0;">{{if eq .FactorType "passkey"}}Passkey{{else}}App di autenticazione (TOTP){{end}}</td></tr>
    <tr><td style="padding:4px 16px 4px 0;color:#6c757d;">Data</td><td style="padding:4px 0;">{{.At}}</td></tr>
    <tr><td style="padding:4px 16px 4px 0;color:#6c757d;">Da</td><td style="padding:4px 0;">{{.RequestIP}}</td></tr>
  </table>
  <p style="color:#6c757d;font-size:14px;">Se è stato lei, non deve fare nulla.{{if .Replaced}} L'autenticatore precedente non funziona più e tutte le altre sessioni attive sono state disconnesse.{{end}}</p>
  <p style="color:#b91c1c;font-size:14px;">Se <strong>non</strong> è stato lei, qualcun altro ha accesso al suo account: cambi subito la password e contatti <a href="mailto:{{.SupportEmail}}" style="color:#b91c1c;">{{.SupportEmail}}</a>.</p>
  <hr style="border:none;border-top:1px solid #e0e0e0;margin:32px 0;">
  <p style="color:#9ca3af;font-size:12px;">Questa è una notifica di sicurezza e non può essere disattivata.</p>
</body>
</html>`,
	},
}
