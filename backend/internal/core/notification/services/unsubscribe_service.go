package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
)

var ErrUnsubscribeTokenInvalid = errors.New("notification: unsubscribe token invalid or expired")

// ErrOptoutNotRecorded is the only failure Consume reports. Everything else
// in the sequence is recoverable and marked on the token; this one is not,
// because without the durable opt-out there is nothing to recover from — so
// the token is deliberately left unspent and the recipient can click again.
var ErrOptoutNotRecorded = errors.New("notification: marketing opt-out could not be recorded")

// UnsubscribeService manages the creation and consumption of per-email
// unsubscribe tokens that back the mandatory footer link.
type UnsubscribeService interface {
	// IssueToken creates a new unsubscribe token bound to an email address
	// (and optionally a user + category) and returns the raw token string
	// to embed in the unsubscribe URL. context is an opaque producer string
	// (e.g. a campaign/run ref) stored on the token and handed back to a
	// MarketingUnsubscribeSink on consume; pass "" when not needed.
	IssueToken(ctx context.Context, userUUID, address, category, context string) (string, error)

	// ConsumeToken verifies a raw token and returns the stored document.
	// The caller is expected to apply the preference change (or
	// suppression) and then call MarkUsed.
	ConsumeToken(ctx context.Context, raw string) (*models.UnsubscribeTokenDoc, error)

	// MarkUsed flags the token as consumed.
	MarkUsed(ctx context.Context, raw string) error

	// Consume applies a raw unsubscribe token in one ordered, crash-safe
	// sequence: the durable opt-out is recorded first, the token is then
	// claimed atomically, and only afterwards are the preference row and
	// the sink replayed. See the implementation for why that order is the
	// point of the whole method.
	//
	// It returns an error ONLY when the durable opt-out could not be
	// written (ErrOptoutNotRecorded). An unknown, expired or already-used
	// token, a preference that would not write and a sink that is down are
	// all a nil error: the answer to the recipient is generic either way,
	// and whatever is left undone is marked on the token for the reconciler
	// rather than reported to a mail client that cannot act on it.
	Consume(ctx context.Context, raw string) error
}

// MarketingUnsubscribeFirer hands a consumed opt-out to the sink a module
// registered and reports whether it arrived.
// (*NotificationService).FireMarketingUnsubscribe has exactly this shape; it
// is a function rather than an interface because the notification service
// takes this service as a constructor argument, so the reference can only be
// resolved at call time.
type MarketingUnsubscribeFirer func(ctx context.Context, address, category, refContext string) error

// OnMarketingUnsubscribe makes a firer usable wherever an
// iface.MarketingUnsubscribeSink is expected — the reconciler's seam — so the
// live path and the replay path go through the same
// FireMarketingUnsubscribe, and therefore through the same nil-sink and
// panic guards. Without this the reconciler would need its own copy of them,
// and two copies of a guard drift.
func (f MarketingUnsubscribeFirer) OnMarketingUnsubscribe(ctx context.Context, address, category, refContext string) error {
	return f(ctx, address, category, refContext)
}

// UnsubscribeOption wires one of the collaborators Consume needs. IssueToken
// and the token bookkeeping need none of them, which is why they are options
// rather than constructor parameters.
type UnsubscribeOption func(*unsubscribeService)

// WithOptouts wires the durable opt-out repository. Without it Consume fails
// closed — an unwired seam is not "nothing to record".
func WithOptouts(o repository.MarketingOptoutRepository) UnsubscribeOption {
	return func(s *unsubscribeService) { s.optouts = o }
}

// WithPreferences wires the preference service used for tokens that carry a
// user.
func WithPreferences(p PreferenceService) UnsubscribeOption {
	return func(s *unsubscribeService) { s.prefs = p }
}

// WithUnsubscribeSink wires the firer that mirrors the opt-out downstream.
func WithUnsubscribeSink(f MarketingUnsubscribeFirer) UnsubscribeOption {
	return func(s *unsubscribeService) { s.fireSink = f }
}

// WithUnsubscribeLogger overrides the default logger.
func WithUnsubscribeLogger(l *slog.Logger) UnsubscribeOption {
	return func(s *unsubscribeService) {
		if l != nil {
			s.logger = l
		}
	}
}

type unsubscribeService struct {
	repo repository.UnsubscribeRepository
	ttl  time.Duration

	// Consume's collaborators. optouts nil ⇒ Consume fails closed; prefs
	// nil ⇒ a token carrying a user leaves prefPending up; fireSink nil ⇒
	// no consumer is registered, so there is nothing to mirror and nothing
	// to keep pending.
	optouts  repository.MarketingOptoutRepository
	prefs    PreferenceService
	fireSink MarketingUnsubscribeFirer
	logger   *slog.Logger
}

func NewUnsubscribeService(repo repository.UnsubscribeRepository, opts ...UnsubscribeOption) UnsubscribeService {
	s := &unsubscribeService{
		repo:   repo,
		ttl:    30 * 24 * time.Hour,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *unsubscribeService) IssueToken(ctx context.Context, userUUID, address, category, context string) (string, error) {
	raw, err := generateRandomToken(32)
	if err != nil {
		return "", err
	}
	doc := &models.UnsubscribeTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		TokenHash: hashToken(raw),
		UserUUID:  userUUID,
		Address:   address,
		Category:  category,
		Context:   context,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
	}
	if err := s.repo.Create(ctx, doc); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *unsubscribeService) ConsumeToken(ctx context.Context, raw string) (*models.UnsubscribeTokenDoc, error) {
	if raw == "" {
		return nil, ErrUnsubscribeTokenInvalid
	}
	doc, err := s.repo.GetByHash(ctx, hashToken(raw))
	if err != nil {
		return nil, ErrUnsubscribeTokenInvalid
	}
	if doc.UsedAt != nil {
		return nil, ErrUnsubscribeTokenInvalid
	}
	if time.Now().After(doc.ExpiresAt) {
		return nil, ErrUnsubscribeTokenInvalid
	}
	return doc, nil
}

func (s *unsubscribeService) MarkUsed(ctx context.Context, raw string) error {
	return s.repo.MarkUsed(ctx, hashToken(raw))
}

// Consume is the ordered sequence behind a one-click unsubscribe. It is
// deliberately NOT a transaction: each step is ordered so that a crash
// immediately after it leaves a state the recipient's next click heals.
//
//  1. Record the durable opt-out. Crash here and the opt-out is a fact while
//     the token is still unused, so a second click replays an idempotent
//     upsert. If this write fails nothing else runs — the token is left
//     unspent rather than burned for nothing.
//  2. Claim the token atomically, which also raises sinkPending. Crash here
//     and the token is spent but marked: the reconciler still knows the sink
//     never ran. Two concurrent clicks produce exactly one consume; the
//     loser gets (nil, nil), which is not an error.
//  3. Apply the preference, if the token carries a user.
//  4. Fire the sink inline.
//
// Steps 3 and 4 may fail without failing the request, because the opt-out —
// the fact that actually stops the mail — is already durable. What they
// leave undone is MARKED on the token (prefPending / sinkPending) for the
// reconciler; a failure that is only logged is the bug this design exists to
// prevent, and a success that leaves its flag up would be retried forever.
//
// Nothing here logs the raw token or a full address: the token's uuid is the
// only identifier that reaches a log line.
func (s *unsubscribeService) Consume(ctx context.Context, raw string) error {
	if s.optouts == nil {
		// Fail-closed, and checked before anything else — including the
		// empty-token shortcut — because an unwired seam is not "there is
		// no opt-out to record", it is "consent is being ignored". It is a
		// boot-time misconfiguration that answers every token identically,
		// so refusing here reveals nothing about any one of them, and it
		// lets a wiring test catch the mistake without a database.
		s.logger.Error("notification: unsubscribe consume refused, opt-out repository not wired")
		return fmt.Errorf("%w: opt-out repository not wired", ErrOptoutNotRecorded)
	}
	if raw == "" {
		return nil
	}
	hash := hashToken(raw)

	// The address the opt-out is keyed by lives on the token, and step 1
	// has to run before the token is spent — so this read must NOT consume.
	doc, err := s.repo.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// No such token. Not an error: the public response is generic
			// regardless, and failing here would build an oracle that tells
			// a caller whether a token was ever real.
			return nil
		}
		return fmt.Errorf("%w: %w", ErrOptoutNotRecorded, err)
	}

	category := doc.Category
	if category == "" {
		category = models.TypeMarketing // empty on the token = all marketing
	}
	if strings.TrimSpace(doc.Address) == "" {
		// Upsert returns nil for an empty address — it reports success for
		// a write that never happened — so the address is checked here
		// rather than inferred from its error. Claiming the token now would
		// burn it with nothing recorded.
		s.logger.Warn("notification: unsubscribe token carries no address",
			slog.String("tokenUuid", doc.UUID))
		return fmt.Errorf("%w: token %s carries no address", ErrOptoutNotRecorded, doc.UUID)
	}

	now := time.Now()

	// Step 1 — the durable fact, first.
	if err := s.optouts.Upsert(ctx, models.MarketingOptoutDoc{
		Address:         doc.Address,
		Category:        category,
		At:              now,
		SourceTokenUUID: doc.UUID,
	}); err != nil {
		// Scrubbed on both paths — the log line and the error the caller
		// logs in turn — because a write error quotes the filter or the
		// document it failed on. (Not the duplicate-key case: the
		// repository converts that one to nil, since the row already
		// existing is the state this call wanted.)
		reason := scrubAddress(err.Error(), doc.Address)
		s.logger.Error("notification: recording the marketing opt-out failed",
			slog.String("tokenUuid", doc.UUID), slog.String("error", reason))
		return fmt.Errorf("%w: %s", ErrOptoutNotRecorded, reason)
	}

	// Step 2 — spend the token, atomically. The claim also raises the
	// pending flags for everything below it, because a crash after this
	// point has no failure to react to: marking on failure alone would
	// leave a dead process's unfinished work invisible.
	claimed, err := s.repo.ClaimToken(ctx, hash, now, doc.UserUUID != "")
	if err != nil {
		// The opt-out is already durable, which is what the recipient asked
		// for, so this is not their failure. The token stays unclaimed and a
		// replay heals the rest.
		s.logger.Warn("notification: claiming the unsubscribe token failed",
			slog.String("tokenUuid", doc.UUID), slog.String("error", err.Error()))
		return nil
	}
	if claimed == nil {
		// Already used, expired, or gone. The opt-out above covered the only
		// part that matters, so this is a success for the caller.
		return nil
	}

	// Step 3 — the preference row, when the token names a user. prefPending
	// is already up from the claim, so a failure — and equally a crash
	// right here — needs no write to stay visible; only the success has
	// something to record.
	if claimed.UserUUID != "" {
		if err := s.applyPreference(ctx, claimed, category); err != nil {
			s.logger.Warn("notification: unsubscribe preference not applied, left pending for replay",
				slog.String("tokenUuid", claimed.UUID),
				slog.String("error", scrubAddress(err.Error(), claimed.Address)))
		} else {
			s.clearPending(ctx, s.repo.ClearPrefPending, hash, claimed.UUID, "prefPending")
		}
	}

	// Step 4 — the sink, inline. sinkPending is already up from the claim,
	// so only success has anything to write.
	if s.fireSink == nil {
		// No consumer is registered (the base ships none): there is nothing
		// to mirror, and leaving the flag up would have the reconciler
		// retrying against a sink that does not exist.
		s.clearPending(ctx, s.repo.ClearSinkPending, hash, claimed.UUID, "sinkPending")
		return nil
	}
	if err := s.fireSink(ctx, claimed.Address, category, claimed.Context); err != nil {
		// Scrubbed: core just handed this sink the address, and a sink that
		// quotes it back in its error is the ordinary case, not an exotic
		// one.
		s.logger.Warn("notification: unsubscribe sink failed, left pending for replay",
			slog.String("tokenUuid", claimed.UUID),
			slog.String("error", scrubAddress(err.Error(), claimed.Address)))
		return nil
	}
	s.clearPending(ctx, s.repo.ClearSinkPending, hash, claimed.UUID, "sinkPending")
	return nil
}

// applyPreference opts the token's user out of the category on email.
func (s *unsubscribeService) applyPreference(ctx context.Context, doc *models.UnsubscribeTokenDoc, category string) error {
	if s.prefs == nil {
		return errors.New("preference service not wired")
	}
	return s.prefs.Set(ctx, doc.UserUUID, category, models.ChannelEmail, false)
}

// clearPending lowers one of the flags the claim raised, now that the work
// behind it is done. A failure here is safe in the direction that matters —
// the reconciler replays work that is idempotent — so it is logged, not
// returned.
func (s *unsubscribeService) clearPending(ctx context.Context, clear func(context.Context, string) error, hash, tokenUUID, flag string) {
	if err := clear(ctx, hash); err != nil {
		s.logger.Warn("notification: could not lower a pending flag after its work succeeded",
			slog.String("tokenUuid", tokenUUID), slog.String("flag", flag),
			slog.String("error", err.Error()))
	}
}

// scrubAddress renders a message safe to log and to hand back. Two sources
// put a recipient's address inside a string nobody classifies as PII: a
// MongoDB write error quotes the filter or document it failed on, and a
// sink's error commonly quotes the address core just handed it
// ("failed to upsert contact ada@example.test: …"). Both forms of the
// address are replaced — the one the token carries and the normalized one
// the opt-out collection stores.
//
// Only a value that actually looks like an address is substituted: a blind
// ReplaceAll over a one- or two-character field would shred the diagnostic
// this is meant to preserve.
func scrubAddress(msg, address string) string {
	for _, form := range []string{address, repository.NormalizeOptoutAddress(address)} {
		if len(form) >= 3 && strings.Contains(form, "@") {
			msg = strings.ReplaceAll(msg, form, "[address]")
		}
	}
	return msg
}

// generateRandomToken returns a URL-safe random string backed by n bytes.
func generateRandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken returns a hex sha256 of the raw token for database lookup.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
