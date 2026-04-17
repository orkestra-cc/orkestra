package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/shared/iface"
)

// PaymentService is the iface.PaymentProvider façade registered in the
// ServiceRegistry. It routes calls to the appropriate underlying provider
// (currently Stripe only; PayPal slots in here later).
//
// Each method also persists a Transaction record, so the admin UI can
// display history without having to query Stripe directly.
type PaymentService struct {
	providers map[models.ProviderName]iface.PaymentProvider
	defaultP  models.ProviderName
	txRepo    repository.TransactionRepository
	logger    *slog.Logger
}

func NewPaymentService(defaultProvider models.ProviderName, providers map[models.ProviderName]iface.PaymentProvider, txRepo repository.TransactionRepository, logger *slog.Logger) *PaymentService {
	return &PaymentService{
		providers: providers,
		defaultP:  defaultProvider,
		txRepo:    txRepo,
		logger:    logger,
	}
}

func (s *PaymentService) Name() string { return string(s.defaultP) }

func (s *PaymentService) resolve(name string) (iface.PaymentProvider, models.ProviderName, error) {
	key := models.ProviderName(name)
	if key == "" {
		key = s.defaultP
	}
	p, ok := s.providers[key]
	if !ok {
		return nil, "", fmt.Errorf("payments: provider %q not configured", key)
	}
	return p, key, nil
}

// CreateCustomer delegates to the default provider (v1 = stripe).
func (s *PaymentService) CreateCustomer(ctx context.Context, in iface.CustomerInput) (iface.CustomerRef, error) {
	p, _, err := s.resolve("")
	if err != nil {
		return iface.CustomerRef{}, err
	}
	return p.CreateCustomer(ctx, in)
}

func (s *PaymentService) AttachPaymentMethod(ctx context.Context, cust iface.CustomerRef, token string) (iface.PaymentMethodRef, error) {
	p, _, err := s.resolve(cust.Provider)
	if err != nil {
		return iface.PaymentMethodRef{}, err
	}
	return p.AttachPaymentMethod(ctx, cust, token)
}

func (s *PaymentService) ChargeSubscription(ctx context.Context, in iface.SubscriptionCharge) (iface.ChargeResult, error) {
	p, providerKey, err := s.resolve(in.Customer.Provider)
	if err != nil {
		return iface.ChargeResult{}, err
	}
	result, callErr := p.ChargeSubscription(ctx, in)

	// Persist transaction record regardless of outcome — makes the admin
	// view accurate even if the provider call errored before returning a
	// meaningful status.
	tx := &models.Transaction{
		UUID:             uuid.NewString(),
		Provider:         providerKey,
		ProviderTxID:     result.ProviderTxID,
		SubscriptionUUID: in.SubscriptionUUID,
		InvoiceUUID:      in.InvoiceUUID,
		ClientUUID:       customerClientFromMetadata(in.Metadata),
		AmountCents:      in.AmountCents,
		Currency:         in.Currency,
		Description:      in.Description,
		Metadata:         in.Metadata,
	}
	now := time.Now().UTC()
	switch {
	case callErr != nil:
		tx.Status = models.TxFailed
		tx.FailureCode = "provider_error"
		tx.FailureMsg = callErr.Error()
	case result.Status == "succeeded":
		tx.Status = models.TxSucceeded
		tx.ChargedAt = &now
	case result.Status == "requires_action":
		tx.Status = models.TxRequiresAction
	default:
		tx.Status = models.TxFailed
		tx.FailureCode = result.FailureCode
		tx.FailureMsg = result.FailureMsg
	}
	if err := s.txRepo.Create(ctx, tx); err != nil {
		s.logger.Warn("payments: persist transaction failed",
			slog.String("providerTxID", result.ProviderTxID),
			slog.String("error", err.Error()),
		)
	}
	return result, callErr
}

func (s *PaymentService) RefundCharge(ctx context.Context, providerTxID string, amountCents int64, reason string) (iface.RefundResult, error) {
	// Look up the provider that owns this transaction.
	tx, err := s.txRepo.GetByProviderTxID(ctx, s.defaultP, providerTxID)
	var providerKey models.ProviderName = s.defaultP
	if err == nil && tx != nil {
		providerKey = tx.Provider
	}
	p, ok := s.providers[providerKey]
	if !ok {
		return iface.RefundResult{}, errors.New("payments: provider not configured")
	}
	res, refundErr := p.RefundCharge(ctx, providerTxID, amountCents, reason)
	if refundErr != nil {
		return res, refundErr
	}
	if tx != nil {
		now := time.Now().UTC()
		tx.RefundedAt = &now
		tx.RefundedCents += amountCents
		if tx.RefundedCents >= tx.AmountCents || amountCents == 0 {
			tx.Status = models.TxRefunded
			tx.RefundedCents = tx.AmountCents
		} else {
			tx.Status = models.TxPartialRefunded
		}
		if err := s.txRepo.Update(ctx, tx); err != nil {
			s.logger.Warn("payments: update transaction after refund failed",
				slog.String("providerTxID", providerTxID),
				slog.String("error", err.Error()),
			)
		}
	}
	return res, nil
}

func (s *PaymentService) VerifyWebhook(ctx context.Context, rawBody []byte, headers map[string]string) (iface.WebhookEvent, error) {
	// In v1 all webhook traffic is Stripe. The webhook HTTP handler calls
	// the specific provider's VerifyWebhook directly, not through this
	// façade — this method exists to satisfy iface.PaymentProvider.
	return s.providers[s.defaultP].VerifyWebhook(ctx, rawBody, headers)
}

// Provider returns the underlying provider by name, for the webhook handler
// that needs direct access to VerifyWebhook with a specific secret.
func (s *PaymentService) Provider(name models.ProviderName) (iface.PaymentProvider, bool) {
	p, ok := s.providers[name]
	return p, ok
}

func customerClientFromMetadata(md map[string]string) string {
	if md == nil {
		return ""
	}
	return md["clientUUID"]
}
