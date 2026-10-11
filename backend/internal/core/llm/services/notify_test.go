package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

type captureSender struct {
	reqs   []iface.TemplatedNotificationRequest
	status map[string]string // recipient UUID -> forced result status
	err    error
}

func (s *captureSender) IsConfigured(context.Context) bool { return true }
func (s *captureSender) Send(context.Context, iface.NotificationRequest) (*iface.NotificationResult, error) {
	return nil, errors.New("not used")
}
func (s *captureSender) SendTemplated(ctx context.Context, req iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	if _, ok := tenantrepo.CurrentTenantID(ctx); !ok {
		return nil, errors.New("no tenant in ctx")
	}
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return nil, s.err
	}
	st := "sent"
	if v := s.status[req.Recipients[0].UserUUID]; v != "" {
		st = v
	}
	return &iface.NotificationResult{Status: st}, nil
}

type memberDir struct{ members []iface.TenantMemberSummary }

func (d memberDir) ListTenantMembers(context.Context, string) ([]iface.TenantMemberSummary, error) {
	return d.members, nil
}

type userBook map[string]*iface.User

func (b userBook) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	if u, ok := b[id]; ok {
		return u, nil
	}
	return nil, iface.ErrUserNotFound
}

// flakyUsers fails the lookup of the listed users with a transient error.
type flakyUsers struct {
	userBook
	down map[string]bool
}

func (f flakyUsers) GetUserByID(ctx context.Context, id string) (*iface.User, error) {
	if f.down[id] {
		return nil, errors.New("user store: connection reset")
	}
	return f.userBook.GetUserByID(ctx, id)
}

type oneBudget struct{ b *models.Budget }

func (o oneBudget) Get(context.Context) (*models.Budget, error) {
	if o.b == nil {
		return nil, repository.ErrNotFound
	}
	cp := *o.b
	return &cp, nil
}

func notifierFixture(sender *captureSender) (*BudgetNotifier, *bytes.Buffer) {
	dir := memberDir{members: []iface.TenantMemberSummary{
		{UserUUID: "owner-1", IsOwner: true},
		{UserUUID: "owner-2", Roles: []string{"org_owner"}},
		{UserUUID: "admin-1", Roles: []string{"org_admin"}},
		{UserUUID: "member-1", Roles: []string{"org_member"}},
		{UserUUID: "extra-1", Roles: []string{"org_member"}},
		{UserUUID: "ghost", Roles: []string{"org_admin"}},
	}}
	users := userBook{
		"owner-1":  {UUID: "owner-1", Email: "o1@example.test", FullName: "Owner One"},
		"owner-2":  {UUID: "owner-2", Email: "o2@example.test", Language: "it"},
		"admin-1":  {UUID: "admin-1", Email: "a1@example.test"},
		"member-1": {UUID: "member-1", Email: "m1@example.test"},
		"extra-1":  {UUID: "extra-1", Email: "x1@example.test"},
	}
	budget := oneBudget{b: &models.Budget{MonthlyTokenLimit: 1000, WarnPct: 80, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"extra-1", "not-a-member", "owner-1"}}}
	logs := &bytes.Buffer{}
	return NewBudgetNotifier(sender, dir, users, budget, &memDeliveries{}, "https://console.example.test", slog.New(slog.NewTextHandler(logs, nil))), logs
}

// memDeliveries is a DeliveryLog keyed by event, fenced by nothing.
type memDeliveries struct {
	mu   sync.Mutex
	done map[string][]string
}

func (d *memDeliveries) MarkDelivered(_ context.Context, uuid string, _ time.Time, recipient string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done == nil {
		d.done = map[string][]string{}
	}
	d.done[uuid] = append(d.done[uuid], recipient)
	return nil
}

func (d *memDeliveries) of(uuid string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.done[uuid]...)
}

func outboxEvent(threshold string) models.OutboxEvent {
	lease := time.Now().Add(time.Minute)
	return models.OutboxEvent{UUID: "ev-1", TenantID: "t1", Period: "2026-10", Threshold: threshold, LeaseUntil: &lease,
		Payload: map[string]any{"consumedTokens": int64(850), "limitTokens": int64(1000), "pct": int64(85)}}
}

func TestBudgetNotifier_RecipientsTemplatesAndKeys(t *testing.T) {
	sender := &captureSender{}
	n, logs := notifierFixture(sender)
	if err := n.SendBudgetEvent(ctxWithTenant(context.Background(), "t1"), outboxEvent(models.ThresholdWarn)); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range sender.reqs {
		if len(r.Recipients) != 1 {
			t.Fatalf("one recipient per send, got %d", len(r.Recipients))
		}
		rc := r.Recipients[0]
		got = append(got, rc.UserUUID)
		if r.TemplateID != TemplateBudgetWarning || r.Type != "transactional" || r.Category != NotificationCategoryBudget || r.Channel != "email" {
			t.Fatalf("request = %+v", r)
		}
		if r.IdempotencyKey != "llm-budget-ev-1-"+rc.UserUUID+"-0" {
			t.Fatalf("idempotency key = %q", r.IdempotencyKey)
		}
		if r.Data["consoleUrl"] != "https://console.example.test/admin/llm?tab=budget" || r.Data["period"] != "2026-10" || r.Data["pct"] != int64(85) {
			t.Fatalf("data = %v", r.Data)
		}
		wantLocale := "en"
		if rc.UserUUID == "owner-2" {
			wantLocale = "it"
		}
		if r.Locale != wantLocale || rc.Address == "" {
			t.Fatalf("recipient %s locale %q address %q", rc.UserUUID, r.Locale, rc.Address)
		}
	}
	sort.Strings(got)
	// Owners (flag or role), admins and the member extra; the plain member,
	// the non-member extra and the unresolvable admin are left out.
	if want := []string{"admin-1", "extra-1", "owner-1", "owner-2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("an unresolvable recipient must WARN: %s", logs.String())
	}
}

func TestBudgetNotifier_ExhaustedTemplateAndFailedResultRetries(t *testing.T) {
	sender := &captureSender{status: map[string]string{"admin-1": "failed"}}
	n, _ := notifierFixture(sender)
	err := n.SendBudgetEvent(ctxWithTenant(context.Background(), "t1"), outboxEvent(models.ThresholdExhausted))
	if err == nil {
		t.Fatal("a failed delivery must surface so the outbox retries")
	}
	for _, r := range sender.reqs {
		if r.TemplateID != TemplateBudgetExhausted {
			t.Fatalf("template = %s", r.TemplateID)
		}
		if note, _ := r.Data["actionNote"].(string); !strings.Contains(note, "refused") && !strings.Contains(note, "rifiutata") {
			t.Fatalf("block note = %q", note)
		}
	}
	if len(sender.reqs) != 4 {
		t.Fatalf("every recipient is still attempted: %d", len(sender.reqs))
	}
}

func TestBudgetNotifier_NoSenderIsANoop(t *testing.T) {
	n, _ := notifierFixture(&captureSender{})
	n.sender = nil
	if err := n.SendBudgetEvent(ctxWithTenant(context.Background(), "t1"), outboxEvent(models.ThresholdWarn)); err != nil {
		t.Fatalf("no sender must not retry forever: %v", err)
	}
}

// Every declared template parses as Go text/template (subject, text) and
// renders with the data the notifier sends.
func TestBudgetTemplates_ParseAndRender(t *testing.T) {
	sender := &captureSender{}
	n, _ := notifierFixture(sender)
	for _, th := range []string{models.ThresholdWarn, models.ThresholdExhausted} {
		if err := n.SendBudgetEvent(ctxWithTenant(context.Background(), "t1"), outboxEvent(th)); err != nil {
			t.Fatal(err)
		}
	}
	data := map[string]map[string]any{}
	for _, r := range sender.reqs {
		data[r.TemplateID+"|"+r.Locale] = r.Data
	}
	specs := BudgetNotificationTemplates()
	if len(specs) != 4 {
		t.Fatalf("templates = %d, want 2 ids x en,it", len(specs))
	}
	for _, s := range specs {
		d := data[s.TemplateID+"|en"]
		for _, src := range []string{s.Subject, s.BodyText, s.BodyHTML} {
			tpl, err := template.New("x").Option("missingkey=error").Parse(src)
			if err != nil {
				t.Fatalf("%s/%s: %v", s.TemplateID, s.Locale, err)
			}
			var out bytes.Buffer
			if err := tpl.Execute(&out, d); err != nil {
				t.Fatalf("%s/%s render: %v", s.TemplateID, s.Locale, err)
			}
			if strings.Contains(out.String(), "<no value>") {
				t.Fatalf("%s/%s renders a missing variable: %s", s.TemplateID, s.Locale, out.String())
			}
		}
	}
}

// replaySender models the notification module's idempotency: a key seen
// before replays its cached result — a failure included — without sending.
type replaySender struct {
	mu      sync.Mutex
	cache   map[string]string // key -> status
	sends   []string          // recipient of every real send
	failFor map[string]bool   // recipient -> fail the real send
	slow    map[string]bool   // recipient -> block until the send's ctx ends
}

func newReplaySender() *replaySender {
	return &replaySender{cache: map[string]string{}, failFor: map[string]bool{}, slow: map[string]bool{}}
}

func (s *replaySender) IsConfigured(context.Context) bool { return true }
func (s *replaySender) Send(context.Context, iface.NotificationRequest) (*iface.NotificationResult, error) {
	return nil, errors.New("not used")
}
func (s *replaySender) SendTemplated(ctx context.Context, req iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	uid := req.Recipients[0].UserUUID
	s.mu.Lock()
	if st, ok := s.cache[req.IdempotencyKey]; ok {
		s.mu.Unlock()
		return &iface.NotificationResult{Status: st}, nil
	}
	slow := s.slow[uid]
	s.mu.Unlock()
	if slow {
		<-ctx.Done()
		s.mu.Lock()
		s.cache[req.IdempotencyKey] = "failed"
		s.mu.Unlock()
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := "sent"
	if s.failFor[uid] {
		st = "failed"
	} else {
		s.sends = append(s.sends, uid)
	}
	s.cache[req.IdempotencyKey] = st
	return &iface.NotificationResult{Status: st}, nil
}

func (s *replaySender) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.sends...)
	sort.Strings(out)
	return out
}

// A partial failure: the retry sends only to the recipient still missing,
// under a new attempt-scoped key (the failed key would only replay the
// cached failure), and the recipients delivered on the first pass never
// get a second copy — not on the retry, not on a later pass after the
// notification module's idempotency window has long expired.
func TestBudgetNotifier_RetrySendsOnlyToUndeliveredRecipients(t *testing.T) {
	sender := newReplaySender()
	sender.failFor["admin-1"] = true
	n, _ := notifierFixture(&captureSender{})
	n.sender = sender
	log := n.deliveries.(*memDeliveries)
	ctx := ctxWithTenant(context.Background(), "t1")

	ev := outboxEvent(models.ThresholdWarn)
	if err := n.SendBudgetEvent(ctx, ev); err == nil {
		t.Fatal("a failed recipient must surface for the retry")
	}
	if got := strings.Join(sender.sent(), ","); got != "extra-1,owner-1,owner-2" {
		t.Fatalf("first pass delivered %s", got)
	}

	// Attempt 2: the recipient's mail server is back.
	sender.mu.Lock()
	sender.failFor["admin-1"] = false
	sender.mu.Unlock()
	ev.Attempts, ev.DeliveredTo = 1, log.of(ev.UUID)
	if err := n.SendBudgetEvent(ctx, ev); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := strings.Join(sender.sent(), ","); got != "admin-1,extra-1,owner-1,owner-2" {
		t.Fatalf("after retry delivered %s (each recipient exactly once)", got)
	}

	// Much later (cache expired), a spurious re-run of the same event.
	sender.mu.Lock()
	sender.cache = map[string]string{}
	sender.mu.Unlock()
	ev.Attempts, ev.DeliveredTo = 7, log.of(ev.UUID)
	if err := n.SendBudgetEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if got := len(sender.sent()); got != 4 {
		t.Fatalf("a delivered recipient got a second copy: %d sends", got)
	}
}

// One slow mail server costs only its own recipient: the others are
// delivered and recorded, the slow one is retried.
func TestBudgetNotifier_SlowRecipientDoesNotFailTheOthers(t *testing.T) {
	sender := newReplaySender()
	sender.slow["admin-1"] = true
	n, _ := notifierFixture(&captureSender{})
	n.sender, n.sendTimeout = sender, 50*time.Millisecond
	ctx, cancel := context.WithTimeout(ctxWithTenant(context.Background(), "t1"), 5*time.Second)
	defer cancel()
	if err := n.SendBudgetEvent(ctx, outboxEvent(models.ThresholdWarn)); err == nil {
		t.Fatal("the timed-out recipient must be retried")
	}
	got := n.deliveries.(*memDeliveries).of("ev-1")
	sort.Strings(got)
	if strings.Join(got, ",") != "extra-1,owner-1,owner-2" {
		t.Fatalf("recorded = %v", got)
	}
}

// A transient profile lookup failure is not a deleted user: the event
// fails so the outbox retries that recipient, while the others are
// delivered and recorded once.
func TestBudgetNotifier_LookupFailureIsRetried(t *testing.T) {
	sender := newReplaySender()
	n, _ := notifierFixture(&captureSender{})
	n.sender = sender
	book := n.users.(userBook)
	n.users = flakyUsers{userBook: book, down: map[string]bool{"admin-1": true}}
	log := n.deliveries.(*memDeliveries)
	ctx := ctxWithTenant(context.Background(), "t1")

	ev := outboxEvent(models.ThresholdWarn)
	if err := n.SendBudgetEvent(ctx, ev); err == nil {
		t.Fatal("a failed lookup must surface for the retry")
	}
	if got := strings.Join(sender.sent(), ","); got != "extra-1,owner-1,owner-2" {
		t.Fatalf("first pass delivered %s", got)
	}
	n.users = book
	ev.Attempts, ev.DeliveredTo = 1, log.of(ev.UUID)
	if err := n.SendBudgetEvent(ctx, ev); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := strings.Join(sender.sent(), ","); got != "admin-1,extra-1,owner-1,owner-2" {
		t.Fatalf("after retry delivered %s", got)
	}
}
