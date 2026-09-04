package services

import (
	"context"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Inside the cap an attacker can force at most three re-issues per
// address per window; the fourth request must leave the victim's live
// token alone. Today every call invalidates it, so a script can keep a
// reset link permanently dead.
func TestForgotPassword_OverEmailCapIssuesNothing(t *testing.T) {
	svc, tokens, mail := newRequestCapTestService(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.20"); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	issuedAfterCap := tokens.createCount()
	invalidatedAfterCap := tokens.invalidateCount()
	// Each in-cap request enqueues one send. Wait for all three to have
	// STARTED before measuring, so the baseline below is a settled 3.
	mail.waitForEnqueued(t, 3)

	// The fourth is over the cap: generic success, and NOTHING happens.
	if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.20"); err != nil {
		t.Fatalf("over-cap request must still answer generically: %v", err)
	}
	if tokens.createCount() != issuedAfterCap {
		t.Error("an over-cap request must not mint a token")
	}
	if tokens.invalidateCount() != invalidatedAfterCap {
		t.Error("an over-cap request must not invalidate the victim's live token")
	}
	// A send queued by the over-cap request would push the count past
	// the settled 3. This read is deliberately immediate: it can only
	// under-report a violation, never invent one, so it cannot flake.
	if got := mail.notifier.count(); got != 3 {
		t.Errorf("an over-cap request must not send mail: %d sends started, want 3", got)
	}
}

// The cost must be identical for a known and an unknown address, so the
// counter is charged BEFORE the lookup.
func TestForgotPassword_RecordsBeforeTheLookup(t *testing.T) {
	svc, _, _ := newRequestCapTestService(t)
	counter := svc.attempts
	ctx := context.Background()

	_ = svc.ForgotPassword(ctx, "nobody@example.com", "203.0.113.21")
	v, err := counter.Locked(ctx, AttemptKeyResetEmail(PolicyAudienceOperator, "nobody@example.com"), ResetRequestsPerEmail)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != 1 {
		t.Fatalf("count for an UNKNOWN address = %d, want 1 — the cap must be charged before the lookup", v.Count)
	}
}

// The handler must not wait on the relay. With a sender that blocks, the
// call still returns promptly.
func TestForgotPassword_DoesNotWaitOnDelivery(t *testing.T) {
	svc, _, mail := newRequestCapTestServiceBlockingSender(t)
	ctx := context.Background()

	start := time.Now()
	if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.22"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("ForgotPassword took %v with a blocking sender; the send must be detached", elapsed)
	}
	mail.waitForEnqueued(t, 1)
}

// M-6: a verification resend is not a login failure and must never be
// able to lock a login.
//
// PasswordAuthService no longer holds a shared/errors.RateLimiter at
// all (H-1, Task 11) — the pre-fix code's "ip:"/"email:" writes via
// `s.rateLimiter.IsBlocked`/`RecordFailedAuth` went to a store that has
// since been deleted outright, so "resend never touches the login
// bucket" is enforced by construction: there is no bucket left for it
// to touch. The assertions below pin what remains — the AttemptCounter
// LOGIN scopes (email/IP) stay untouched by a resend.
func TestResendVerification_NeverTouchesTheLoginScopes(t *testing.T) {
	svc, _, _, _ := newRequestCapFixture(t, false)
	counter := svc.attempts
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_ = svc.ResendVerification(ctx, "known@example.com", "203.0.113.23")
	}

	emailV, _ := counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 5, Window: time.Minute})
	if emailV.Count != 0 {
		t.Fatalf("login email scope (AttemptCounter) = %d after 10 resends, want 0", emailV.Count)
	}
	ipV, _ := counter.Locked(ctx, AttemptKeyIP("203.0.113.23"), Limit{Threshold: 100, Window: time.Minute})
	if ipV.Count != 0 {
		t.Fatalf("login IP scope (AttemptCounter) = %d after 10 resends, want 0", ipV.Count)
	}

	// It DOES charge its own scope.
	verifyV, _ := counter.Locked(ctx, AttemptKeyVerifyEmail(PolicyAudienceOperator, "known@example.com"), VerifyRequestsPerEmail)
	if verifyV.Count == 0 {
		t.Fatal("the resend must charge its own verify-email scope")
	}
}

// The peek must not consume: the old IsBlocked pre-check spent a token
// on every call, which is how an anonymous caller could pin any address
// at 429 forever without ever failing anything.
func TestResendVerification_PeekDoesNotConsume(t *testing.T) {
	svc, tokens, _ := newRequestCapTestService(t)
	ctx := context.Background()

	// Three accepted requests are the cap; the counter must read exactly
	// 3, not 6 (peek + record on each).
	for i := 0; i < 3; i++ {
		_ = svc.ResendVerification(ctx, "unverified@example.com", "203.0.113.24")
	}
	v, _ := svc.attempts.Locked(ctx, AttemptKeyVerifyEmail(PolicyAudienceOperator, "unverified@example.com"), VerifyRequestsPerEmail)
	if v.Count != 3 {
		t.Fatalf("verify-email count = %d after 3 requests, want exactly 3", v.Count)
	}
	if tokens.createCount() != 3 {
		t.Fatalf("issued %d tokens for 3 in-cap requests, want 3", tokens.createCount())
	}
}

// A request cap must never surface as an error; the endpoint's single
// generic answer is what makes it non-enumerable.
func TestRequestCaps_AlwaysAnswerGenerically(t *testing.T) {
	svc, _, _ := newRequestCapTestService(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.25"); err != nil {
			t.Fatalf("ForgotPassword %d returned %v; only the method gate may return an error", i, err)
		}
		if err := svc.ResendVerification(ctx, "known@example.com", "203.0.113.25"); err != nil {
			t.Fatalf("ResendVerification %d returned %v", i, err)
		}
	}
}

// ===== fixtures =====
//
// newRequestCapTestService wires a PasswordAuthService whose attempt
// counter, email-token repository and mail dispatcher are all
// observable: a MemoryAttemptCounter (Task 3), a recording
// EmailTokenRepository counting Create/InvalidateByUserAndPurpose, and
// a REAL, started *MailDispatcher (Task 6) fronted by a notifier that
// counts every SendTemplated call. That notifier is also what
// ResendVerification's synchronous sendVerificationEmail path uses —
// deliberately: no test here exercises both an in-cap ForgotPassword
// AND a send-triggering ResendVerification on the same service
// instance, so one counter is enough and stays honest about what
// "enqueued" means (see recordingMail.enqueued's doc comment).
//
// "known@example.com" mirrors the standard verified/active user every
// other gate-test fixture in this package seeds via activeUser();
// "unverified@example.com" is the one user this task's tests need that
// Task 7's lockout fixture does not seed. The inactive/service/oauth-
// only/locked users from Task 7's newLockoutFixture play no role in
// ForgotPassword or ResendVerification, so they are deliberately not
// duplicated here.

func newRequestCapTestService(t *testing.T) (*PasswordAuthService, *recordingEmailTokenRepo, *recordingMail) {
	t.Helper()
	svc, tokens, mail, _ := newRequestCapFixture(t, false)
	return svc, tokens, mail
}

func newRequestCapTestServiceBlockingSender(t *testing.T) (*PasswordAuthService, *recordingEmailTokenRepo, *recordingMail) {
	t.Helper()
	svc, tokens, mail, _ := newRequestCapFixture(t, true)
	return svc, tokens, mail
}

// newRequestCapFixture also returns the *gatesEnv so a test that needs
// to reach a collaborator no thin wrapper exposes (env.users, env.audit,
// etc.) can call this directly instead.
func newRequestCapFixture(t *testing.T, blockSend bool) (*PasswordAuthService, *recordingEmailTokenRepo, *recordingMail, *gatesEnv) {
	t.Helper()

	tokens := newRecordingEmailTokenRepo()
	mail := newRecordingMail(t, blockSend)

	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil, func(cfg *PasswordAuthConfig) {
		cfg.AttemptCounter = NewMemoryAttemptCounter()
		cfg.EmailTokenRepo = tokens
		cfg.Notifier = mail.notifier
		cfg.MailDispatcher = mail.dispatcher
	})

	env.users.seed(activeUser("known@example.com", ""))
	unverified := activeUser("unverified@example.com", "")
	unverified.EmailVerified = false
	env.users.seed(unverified)

	return env.auth, tokens, mail, env
}

// recordingEmailTokenRepo is the repository.EmailTokenRepository the
// request-cap tests observe token issuance/invalidation through. Only
// Create and InvalidateByUserAndPurpose are reached by ForgotPassword /
// ResendVerification; everything else panics — the same discipline
// gateEmailTokenRepo (Task 7's fixtures) uses, so a refactor that takes
// a new dependency surfaces immediately rather than silently no-oping.
type recordingEmailTokenRepo struct {
	mu          sync.Mutex
	created     int
	invalidated int
}

func newRecordingEmailTokenRepo() *recordingEmailTokenRepo {
	return &recordingEmailTokenRepo{}
}

func (r *recordingEmailTokenRepo) Create(context.Context, *authModels.EmailTokenDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created++
	return nil
}

func (r *recordingEmailTokenRepo) InvalidateByUserAndPurpose(context.Context, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidated++
	return nil
}

func (r *recordingEmailTokenRepo) GetByHash(context.Context, string) (*authModels.EmailTokenDoc, error) {
	panic("GetByHash not used by the request-cap tests")
}

func (r *recordingEmailTokenRepo) MarkUsed(context.Context, string) error {
	panic("MarkUsed not used by the request-cap tests")
}

func (r *recordingEmailTokenRepo) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("DeleteAllByUser not used by the request-cap tests")
}

func (r *recordingEmailTokenRepo) createCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.created
}

func (r *recordingEmailTokenRepo) invalidateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.invalidated
}

// recordingMail fronts a REAL, started *MailDispatcher (Task 6) with a
// notifier that counts every SendTemplated call it receives. Nothing
// mocks the dispatcher itself: ForgotPassword's Enqueue call runs
// against the genuine queue/worker-pool implementation, and only the
// terminal send is instrumented.
type recordingMail struct {
	dispatcher *MailDispatcher
	notifier   *recordingNotifier
}

func newRecordingMail(t *testing.T, blockSend bool) *recordingMail {
	t.Helper()
	d := NewMailDispatcher(silentLogger())
	d.Start()
	t.Cleanup(func() { d.Stop(context.Background()) })

	n := &recordingNotifier{}
	if blockSend {
		n.block = make(chan struct{})
		t.Cleanup(func() { close(n.block) })
	}
	return &recordingMail{dispatcher: d, notifier: n}
}

// waitForEnqueued blocks until the dispatcher's worker pool has started
// exactly `want` sends, or fails the test after two seconds. Enqueue is
// asynchronous (that is the whole point of D5), so a check made
// immediately after ForgotPassword returns would race the worker.
//
// It waits for a KNOWN count rather than for quiescence: two reads
// agreeing only means nothing moved between them, which is also true
// before the first worker has run, so a quiescence poll can report a
// settled 0 that is really "not started yet". The deadline is generous
// because it is never reached on a healthy run — it exists to turn a
// genuine failure into a message instead of a hang, so lengthening it
// can only make the test more reliable, never less. For the
// blocking-sender fixture the counter is incremented BEFORE the block,
// so this returns as soon as the send starts even though it never
// completes.
func (m *recordingMail) waitForEnqueued(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got int
	for {
		got = m.notifier.count()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited 2s for %d enqueued send(s), the dispatcher started %d", want, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// recordingNotifier is the iface.NotificationSender both ForgotPassword
// (via the dispatcher) and ResendVerification (synchronously) send
// through in these tests. block, when non-nil, makes SendTemplated hang
// until the fixture's cleanup closes it — after incrementing the
// counter — so TestForgotPassword_DoesNotWaitOnDelivery can prove
// Enqueue never waits on the send it queues.
type recordingNotifier struct {
	mu    sync.Mutex
	sent  int
	block chan struct{}
}

func (n *recordingNotifier) IsConfigured(context.Context) bool { return true }

func (n *recordingNotifier) Send(context.Context, iface.NotificationRequest) (*iface.NotificationResult, error) {
	panic("Send not used by the request-cap tests")
}

func (n *recordingNotifier) SendTemplated(context.Context, iface.TemplatedNotificationRequest) (*iface.NotificationResult, error) {
	n.mu.Lock()
	n.sent++
	block := n.block
	n.mu.Unlock()
	if block != nil {
		<-block
	}
	return &iface.NotificationResult{Status: "sent"}, nil
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.sent
}
