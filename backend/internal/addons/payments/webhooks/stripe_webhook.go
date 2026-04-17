package webhooks

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/orkestra/backend/internal/addons/payments/models"
	"github.com/orkestra/backend/internal/addons/payments/services"
)

// StripeHandler serves POST /v1/payments/webhooks/stripe.
//
// Registered directly on the chi router (not Huma) because HMAC verification
// needs access to the raw request body — Huma's binding would have already
// parsed and discarded it.
type StripeHandler struct {
	payments   *services.PaymentService
	dispatcher *services.Dispatcher
	logger     *slog.Logger
}

func NewStripeHandler(payments *services.PaymentService, dispatcher *services.Dispatcher, logger *slog.Logger) *StripeHandler {
	return &StripeHandler{payments: payments, dispatcher: dispatcher, logger: logger}
}

func (h *StripeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	provider, ok := h.payments.Provider(models.ProviderStripe)
	if !ok {
		h.logger.Warn("stripe webhook: provider not configured")
		http.Error(w, "stripe not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		h.logger.Warn("stripe webhook: read body failed", slog.String("error", err.Error()))
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	headers := map[string]string{
		"Stripe-Signature": r.Header.Get("Stripe-Signature"),
	}
	evt, err := provider.VerifyWebhook(r.Context(), body, headers)
	if err != nil {
		h.logger.Warn("stripe webhook: verification failed", slog.String("error", err.Error()))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	if err := h.dispatcher.Handle(r.Context(), evt); err != nil {
		if errors.Is(err, services.ErrAlreadyProcessed) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"duplicate"}`))
			return
		}
		h.logger.Error("stripe webhook: dispatcher failed",
			slog.String("eventID", evt.ProviderEventID),
			slog.String("error", err.Error()),
		)
		// Return 200 anyway — we stored the event, don't want Stripe to
		// retry indefinitely for a reconcile bug. Admin can replay manually.
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
