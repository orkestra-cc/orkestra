package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Budget notification identity. Template ids follow the SDK convention
// "<module>.<event>"; the category is the routing family sender profiles
// match (llm.*). Budget alerts are transactional: an operator cannot opt
// out of being told the org stopped (or is about to stop) answering.
const (
	TemplateBudgetWarning      = "llm.budget_warning"
	TemplateBudgetExhausted    = "llm.budget_exhausted"
	NotificationCategoryBudget = "llm.budget"
)

// UserLookup resolves a recipient's address and language
// (iface.UserProvider satisfies it).
type UserLookup interface {
	GetUserByID(ctx context.Context, id string) (*iface.User, error)
}

// BudgetReader reads the org's budget (repository.ErrNotFound: none).
type BudgetReader interface {
	Get(ctx context.Context) (*models.Budget, error)
}

// DeliveryLog records per-recipient progress on the outbox event, fenced
// by the worker's lease (repository.Outbox satisfies it).
type DeliveryLog interface {
	MarkDelivered(ctx context.Context, uuid string, leaseUntil time.Time, recipient string) error
}

var (
	_ Notifier    = (*BudgetNotifier)(nil)
	_ DeliveryLog = (*repository.Outbox)(nil)
)

// recipientSendTimeout bounds one recipient's send, so a slow mail server
// cannot spend the whole lease on one address and doom the rest.
const recipientSendTimeout = 15 * time.Second

// BudgetNotifier turns an outbox event into one templated email per
// recipient: the org's owners (IsOwner or the org_owner role) and admins
// (org_admin) — notification targeting, not authorization (ruling 4.12) —
// plus the budget's extra recipients that are still members.
//
// Delivery is tracked per recipient on the outbox row (DeliveredTo, written
// under the lease right after each successful send), so a retry after a
// partial failure sends only to the recipients still missing, however much
// later it runs. Each send's idempotency key is scoped to the attempt,
// llm-budget-<eventUUID>-<recipientUUID>-<attempts>: the notification
// module replays a key's cached result for an hour, failures included, so
// a key reused across retries would replay the failure instead of sending.
// The same key within one attempt (a worker that crashed between the send
// and recording it, re-leased without an attempt increment) replays the
// delivery instead of duplicating it.
type BudgetNotifier struct {
	sender      iface.NotificationSender
	dir         iface.TenantDirectoryReader
	users       UserLookup
	budgets     BudgetReader
	deliveries  DeliveryLog
	consoleURL  string
	sendTimeout time.Duration
	logger      *slog.Logger
}

// NewBudgetNotifier: consoleOrigin is the operator console's public origin
// (the budget page link is built from it). A nil sender or user lookup
// makes every event a logged no-op.
func NewBudgetNotifier(sender iface.NotificationSender, dir iface.TenantDirectoryReader, users UserLookup, budgets BudgetReader, deliveries DeliveryLog, consoleOrigin string, logger *slog.Logger) *BudgetNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	url := ""
	if o := strings.TrimRight(consoleOrigin, "/"); o != "" {
		url = o + "/admin/llm?tab=budget"
	}
	return &BudgetNotifier{sender: sender, dir: dir, users: users, budgets: budgets, deliveries: deliveries, consoleURL: url, sendTimeout: recipientSendTimeout, logger: logger}
}

// actionNotes explains, per locale, what the threshold means for new
// calls. The exhausted alert fires on the first refused call under block
// (ruling 4.1) or when consumption reaches the limit.
var actionNotes = map[string]map[string]string{
	"en": {
		models.ThresholdWarn + "|" + models.BudgetActionBlock:         "When the budget is reached, calls that do not fit the remaining balance are refused.",
		models.ThresholdWarn + "|" + models.BudgetActionWarnOnly:      "The budget is set to warn only: calls continue after it is reached.",
		models.ThresholdExhausted + "|" + models.BudgetActionBlock:    "A call was refused because it no longer fits the remaining balance; new calls are refused until the next period or a higher limit.",
		models.ThresholdExhausted + "|" + models.BudgetActionWarnOnly: "The budget is set to warn only: calls continue, beyond the limit.",
	},
	"it": {
		models.ThresholdWarn + "|" + models.BudgetActionBlock:         "Raggiunto il budget, le chiamate che non rientrano nel saldo residuo vengono rifiutate.",
		models.ThresholdWarn + "|" + models.BudgetActionWarnOnly:      "Il budget è impostato solo come avviso: le chiamate continuano anche dopo averlo raggiunto.",
		models.ThresholdExhausted + "|" + models.BudgetActionBlock:    "Una chiamata è stata rifiutata perché non rientra più nel saldo residuo; le nuove chiamate vengono rifiutate fino al periodo successivo o a un limite più alto.",
		models.ThresholdExhausted + "|" + models.BudgetActionWarnOnly: "Il budget è impostato solo come avviso: le chiamate continuano, oltre il limite.",
	},
}

func localeOf(u *iface.User) string {
	if strings.HasPrefix(strings.ToLower(u.Language), "it") {
		return "it"
	}
	return "en"
}

// SendBudgetEvent delivers ev to every recipient not yet in
// ev.DeliveredTo. It returns an error — so the outbox retries the rest —
// when the recipients cannot be resolved, a send failed or timed out, or
// the event's deadline ended before every recipient was tried;
// unresolvable users are skipped with a WARN.
func (n *BudgetNotifier) SendBudgetEvent(ctx context.Context, ev models.OutboxEvent) error {
	if n.sender == nil || n.users == nil {
		n.logger.WarnContext(ctx, "llm: no notification sender or user directory; budget alert dropped", slog.String("event", ev.UUID), slog.String("threshold", ev.Threshold))
		return nil
	}
	template := TemplateBudgetWarning
	if ev.Threshold == models.ThresholdExhausted {
		template = TemplateBudgetExhausted
	}
	action := models.BudgetActionBlock
	var extra []string
	b, err := n.budgets.Get(ctx)
	switch err = mapRepoErr(err); {
	case err == nil:
		extra = b.NotifyExtraUserUUIDs
		if b.Action == models.BudgetActionWarnOnly {
			action = models.BudgetActionWarnOnly
		}
	case errors.Is(err, ErrNotFound):
	default:
		return fmt.Errorf("llm: read budget: %w", err)
	}
	members, err := n.dir.ListTenantMembers(ctx, ev.TenantID)
	if err != nil {
		return fmt.Errorf("llm: list members: %w", err)
	}
	isMember := make(map[string]bool, len(members))
	want := map[string]bool{}
	for _, m := range members {
		isMember[m.UserUUID] = true
		if m.IsOwner || hasRole(m.Roles, "org_owner") || hasRole(m.Roles, "org_admin") {
			want[m.UserUUID] = true
		}
	}
	for _, u := range extra {
		if isMember[u] {
			want[u] = true
		}
	}
	for _, u := range ev.DeliveredTo {
		delete(want, u)
	}
	recipients := make([]string, 0, len(want))
	for u := range want {
		recipients = append(recipients, u)
	}
	sort.Strings(recipients)
	if len(recipients) == 0 {
		if len(ev.DeliveredTo) == 0 {
			n.logger.WarnContext(ctx, "llm: budget alert has no recipient", slog.String("event", ev.UUID))
		}
		return nil
	}

	failed := 0
	for i, uid := range recipients {
		if ctx.Err() != nil {
			// The event's own deadline: the rest is retried, untouched.
			return fmt.Errorf("llm: budget alert: deadline reached with %d of %d recipients left", len(recipients)-i, len(recipients))
		}
		u, err := n.users.GetUserByID(ctx, uid)
		if err != nil || u == nil || u.Email == "" {
			n.logger.WarnContext(ctx, "llm: budget alert recipient not resolvable; skipped", slog.String("event", ev.UUID), slog.String("user", uid))
			continue
		}
		locale := localeOf(u)
		data := map[string]any{
			"period": ev.Period, "consumedTokens": ev.Payload["consumedTokens"], "limitTokens": ev.Payload["limitTokens"],
			"pct": ev.Payload["pct"], "consoleUrl": n.consoleURL, "actionNote": actionNotes[locale][ev.Threshold+"|"+action],
		}
		sctx, cancel := context.WithTimeout(ctx, n.sendTimeout)
		res, err := n.sender.SendTemplated(sctx, iface.TemplatedNotificationRequest{
			Channel: "email", Type: "transactional", Category: NotificationCategoryBudget, TemplateID: template, Locale: locale,
			Recipients:     []iface.Recipient{{UserUUID: uid, Address: u.Email, Name: u.FullName}},
			Data:           data,
			IdempotencyKey: fmt.Sprintf("llm-budget-%s-%s-%d", ev.UUID, uid, ev.Attempts),
		})
		cancel()
		switch {
		case err != nil:
			failed++
			n.logger.WarnContext(ctx, "llm: budget alert send failed", slog.String("event", ev.UUID), slog.String("user", uid), slog.String("error", err.Error()))
			continue
		case res != nil && res.Status == "failed":
			failed++
			n.logger.WarnContext(ctx, "llm: budget alert delivery failed", slog.String("event", ev.UUID), slog.String("user", uid))
			continue
		}
		// Delivered (or suppressed by the recipient's own settings): record
		// it so no retry sends it again.
		if n.deliveries == nil || ev.LeaseUntil == nil {
			continue
		}
		if err := n.deliveries.MarkDelivered(ctx, ev.UUID, *ev.LeaseUntil, uid); err != nil {
			if errors.Is(err, repository.ErrLeaseLost) {
				return err // another worker holds the event now; it continues from the recorded progress
			}
			// Unrecorded: a retry may send this recipient a second copy,
			// which beats losing the progress of everyone else.
			failed++
			n.logger.WarnContext(ctx, "llm: budget alert delivery not recorded", slog.String("event", ev.UUID), slog.String("user", uid), slog.String("error", err.Error()))
		}
	}
	if failed > 0 {
		return fmt.Errorf("llm: budget alert: %d of %d deliveries failed", failed, len(recipients))
	}
	return nil
}

func hasRole(roles []string, r string) bool {
	for _, x := range roles {
		if x == r {
			return true
		}
	}
	return false
}

// BudgetNotificationTemplates are the default email templates the module
// declares (the notification module seeds them insert-if-absent). Go
// text/template syntax; html/template escapes the HTML body.
func BudgetNotificationTemplates() []module.NotificationTemplateSpec {
	warnVars := []string{"period", "consumedTokens", "limitTokens", "pct", "actionNote", "consoleUrl"}
	return []module.NotificationTemplateSpec{
		{TemplateID: TemplateBudgetWarning, Locale: "en",
			Subject:     "LLM budget at {{.pct}}% for {{.period}}",
			BodyText:    "The organization used {{.consumedTokens}} of {{.limitTokens}} LLM tokens in {{.period}}. {{.actionNote}}\n\nReview usage: {{.consoleUrl}}",
			BodyHTML:    "<p>The organization used <strong>{{.consumedTokens}}</strong> of {{.limitTokens}} LLM tokens in {{.period}}.</p><p>{{.actionNote}}</p><p><a href=\"{{.consoleUrl}}\">Review usage</a></p>",
			Description: "Sent once per month when the LLM budget crosses its warning threshold", Variables: warnVars},
		{TemplateID: TemplateBudgetWarning, Locale: "it",
			Subject:     "Budget LLM al {{.pct}}% per {{.period}}",
			BodyText:    "L'organizzazione ha usato {{.consumedTokens}} token LLM su {{.limitTokens}} in {{.period}}. {{.actionNote}}\n\nControlla i consumi: {{.consoleUrl}}",
			BodyHTML:    "<p>L'organizzazione ha usato <strong>{{.consumedTokens}}</strong> token LLM su {{.limitTokens}} in {{.period}}.</p><p>{{.actionNote}}</p><p><a href=\"{{.consoleUrl}}\">Controlla i consumi</a></p>",
			Description: "Inviata una volta al mese quando il budget LLM supera la soglia di avviso", Variables: warnVars},
		{TemplateID: TemplateBudgetExhausted, Locale: "en",
			Subject:     "LLM budget reached for {{.period}}",
			BodyText:    "The organization reached its {{.limitTokens}}-token LLM budget for {{.period}} ({{.consumedTokens}} tokens used). {{.actionNote}}\n\nOpen the budget page: {{.consoleUrl}}",
			BodyHTML:    "<p>The organization reached its <strong>{{.limitTokens}}</strong>-token LLM budget for {{.period}} ({{.consumedTokens}} tokens used).</p><p>{{.actionNote}}</p><p><a href=\"{{.consoleUrl}}\">Open the budget page</a></p>",
			Description: "Sent once per month when the LLM budget refuses its first call or consumption reaches the limit", Variables: warnVars},
		{TemplateID: TemplateBudgetExhausted, Locale: "it",
			Subject:     "Budget LLM raggiunto per {{.period}}",
			BodyText:    "L'organizzazione ha raggiunto il budget LLM di {{.limitTokens}} token per {{.period}} ({{.consumedTokens}} token usati). {{.actionNote}}\n\nApri la pagina del budget: {{.consoleUrl}}",
			BodyHTML:    "<p>L'organizzazione ha raggiunto il budget LLM di <strong>{{.limitTokens}}</strong> token per {{.period}} ({{.consumedTokens}} token usati).</p><p>{{.actionNote}}</p><p><a href=\"{{.consoleUrl}}\">Apri la pagina del budget</a></p>",
			Description: "Inviata una volta al mese quando il budget LLM rifiuta la prima chiamata o il consumo raggiunge il limite", Variables: warnVars},
	}
}
