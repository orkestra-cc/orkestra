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
}
