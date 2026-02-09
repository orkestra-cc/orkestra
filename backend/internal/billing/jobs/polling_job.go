package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/billing/models"
	"github.com/orkestra/backend/internal/billing/repository"
	"github.com/orkestra/backend/internal/billing/services"
)

// FatturaPayload represents the parsed payload from OpenAPI SDI response
type FatturaPayload struct {
	FatturaElettronicaBody []struct {
		DatiGenerali struct {
			DatiGeneraliDocumento struct {
				Numero                 string `json:"numero"`
				Data                   string `json:"data"`
				ImportoTotaleDocumento string `json:"importo_totale_documento"`
			} `json:"dati_generali_documento"`
		} `json:"dati_generali"`
	} `json:"fattura_elettronica_body"`
}

// PollingJob handles periodic polling of SDI notifications
type PollingJob struct {
	openAPIClient    services.OpenAPIClient
	invoiceRepo      repository.InvoiceRepository
	notificationRepo repository.NotificationRepository
	logger           *slog.Logger
	interval         time.Duration
	stopChan         chan struct{}
	running          bool
}

// NewPollingJob creates a new polling job
func NewPollingJob(
	openAPIClient services.OpenAPIClient,
	invoiceRepo repository.InvoiceRepository,
	notificationRepo repository.NotificationRepository,
	logger *slog.Logger,
	interval time.Duration,
) *PollingJob {
	return &PollingJob{
		openAPIClient:    openAPIClient,
		invoiceRepo:      invoiceRepo,
		notificationRepo: notificationRepo,
		logger:           logger,
		interval:         interval,
		stopChan:         make(chan struct{}),
	}
}

// Start begins the polling job
func (j *PollingJob) Start(ctx context.Context) {
	if j.running {
		j.logger.Warn("polling job already running")
		return
	}

	j.running = true
	j.logger.Info("starting SDI notification polling job",
		"interval", j.interval.String(),
	)

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	// Run immediately on start
	j.poll(ctx)

	for {
		select {
		case <-ticker.C:
			j.poll(ctx)
		case <-j.stopChan:
			j.logger.Info("stopping SDI notification polling job")
			j.running = false
			return
		case <-ctx.Done():
			j.logger.Info("SDI notification polling job stopped due to context cancellation")
			j.running = false
			return
		}
	}
}

// Stop stops the polling job
func (j *PollingJob) Stop() {
	if j.running {
		close(j.stopChan)
	}
}

// Poll manually triggers a poll (useful for API endpoint)
func (j *PollingJob) Poll(ctx context.Context) error {
	return j.poll(ctx)
}

// SyncReceivedInvoices syncs all invoices (issued and received) from OpenAPI SDI
// The function name is kept for backwards compatibility, but it now syncs both directions
func (j *PollingJob) SyncReceivedInvoices(ctx context.Context) error {
	j.logger.Info("syncing all invoices from OpenAPI SDI")

	// Fetch all invoices from last 30 days (both sent and received)
	fromDate := time.Now().AddDate(0, 0, -30)
	page := 1
	pageSize := 100
	totalImportedIssued := 0
	totalImportedReceived := 0
	totalSkipped := 0

	for {
		invoices, err := j.openAPIClient.GetAllInvoices(ctx, fromDate, page, pageSize)
		if err != nil {
			j.logger.Error("failed to fetch invoices from OpenAPI", "error", err)
			return err
		}

		j.logger.Info("fetched all invoices from OpenAPI",
			"page", page,
			"count", len(invoices.Invoices),
			"total", invoices.Total,
		)

		for _, inv := range invoices.Invoices {
			// Check if we already have this invoice by OpenAPIUUID
			existing, _ := j.invoiceRepo.GetByOpenAPIUUID(ctx, inv.UUID)
			if existing != nil {
				j.logger.Info("invoice already exists by OpenAPIUUID, skipping",
					"uuid", inv.UUID,
					"openAPIUUID", inv.UUID,
					"marking", inv.Marking,
				)
				totalSkipped++
				continue
			}

			// Determine direction based on marking field
			var direction models.InvoiceDirection
			var status models.InvoiceStatus
			var sdiStatus models.SDIStatus

			switch inv.Marking {
			case "sent":
				direction = models.DirectionIssued
				status = models.StatusSent      // Issued invoices were sent to SDI
				sdiStatus = models.SDIStatusNone // Awaiting delivery notification
			case "received":
				direction = models.DirectionReceived
				status = models.StatusDelivered  // Received invoices are already delivered to us
				sdiStatus = models.SDIStatusRC   // Ricevuta di Consegna
			case "delivered":
				direction = models.DirectionIssued
				status = models.StatusDelivered  // Issued invoice was delivered to recipient
				sdiStatus = models.SDIStatusRC   // Ricevuta di Consegna (delivery receipt)
			default:
				j.logger.Warn("unknown invoice marking, skipping",
					"uuid", inv.UUID,
					"marking", inv.Marking,
				)
				totalSkipped++
				continue
			}

			// Extract party info from Sender and Recipient fields
			var cedenteIsCompany, cessionarioIsCompany bool
			var cedenteFiscalID, cedenteName string
			var cessionarioFiscalID, cessionarioName string

			if inv.Sender != nil {
				cedenteFiscalID = inv.Sender.BusinessVATNumberCode
				cedenteName = inv.Sender.BusinessName
				cedenteIsCompany = cedenteName != "" // If has business name, it's a company
				if cedenteName == "" && inv.Sender.Name != "" {
					cedenteName = inv.Sender.Name + " " + inv.Sender.Surname
				}
			}

			if inv.Recipient != nil {
				cessionarioFiscalID = inv.Recipient.BusinessVATNumberCode
				cessionarioName = inv.Recipient.BusinessName
				cessionarioIsCompany = cessionarioName != "" // If has business name, it's a company
				if cessionarioName == "" && inv.Recipient.Name != "" {
					cessionarioName = inv.Recipient.Name + " " + inv.Recipient.Surname
				}
			}

			// Determine document type
			docType := models.DocTypeFattura // Default TD01
			if inv.DocumentType != "" {
				docType = models.DocumentType(inv.DocumentType)
			}

			// Parse payload to extract invoice details (number, date, amount)
			var invoiceNumber string
			var invoiceDate time.Time
			var totalAmount float64

			if inv.Payload != "" {
				var payload FatturaPayload
				if err := json.Unmarshal([]byte(inv.Payload), &payload); err == nil {
					if len(payload.FatturaElettronicaBody) > 0 {
						doc := payload.FatturaElettronicaBody[0].DatiGenerali.DatiGeneraliDocumento
						invoiceNumber = doc.Numero
						if parsedDate, err := time.Parse("2006-01-02", doc.Data); err == nil {
							invoiceDate = parsedDate
						}
						if amount, err := strconv.ParseFloat(doc.ImportoTotaleDocumento, 64); err == nil {
							totalAmount = amount
						}
					}
				} else {
					j.logger.Warn("failed to parse invoice payload",
						"uuid", inv.UUID,
						"error", err,
					)
				}
			}

			// Fallback deduplication: Try to find existing invoice by number
			// This handles cases where OpenAPIUUID doesn't match (e.g., UUID changed, update failed, etc.)
			if invoiceNumber != "" {
				var existingByNumber *models.Invoice

				if inv.Marking == "sent" || inv.Marking == "delivered" {
					// For issued invoices (sent or delivered), find by number + direction
					existingByNumber, _ = j.invoiceRepo.GetByNumber(ctx, invoiceNumber, models.DirectionIssued)
				} else if inv.Marking == "received" && cedenteFiscalID != "" {
					// For received invoices, find by number + supplier fiscal ID
					existingByNumber, _ = j.invoiceRepo.FindByNumberAndSupplierFiscalID(ctx, invoiceNumber, cedenteFiscalID)
				}

				if existingByNumber != nil {
					// Found existing invoice by number - UPDATE it with OpenAPIUUID and status instead of creating duplicate
					j.logger.Info("found existing invoice by number, updating OpenAPIUUID and status",
						"uuid", existingByNumber.UUID,
						"invoiceNumber", invoiceNumber,
						"oldOpenAPIUUID", existingByNumber.OpenAPIUUID,
						"newOpenAPIUUID", inv.UUID,
						"oldStatus", existingByNumber.Status,
						"newStatus", status,
						"marking", inv.Marking,
						"direction", direction,
					)
					if err := j.invoiceRepo.UpdateOpenAPIData(ctx, existingByNumber.UUID, inv.UUID, inv.SDIFileID); err != nil {
						j.logger.Error("failed to update invoice OpenAPI data",
							"uuid", existingByNumber.UUID,
							"error", err,
						)
					}
					// Also update status to reflect the new marking state (e.g., sent -> delivered)
					if err := j.invoiceRepo.UpdateStatus(ctx, existingByNumber.UUID, status, sdiStatus); err != nil {
						j.logger.Error("failed to update invoice status",
							"uuid", existingByNumber.UUID,
							"error", err,
						)
					}
					totalSkipped++
					continue
				}
			}

			// Fetch the full XML content (only for genuinely new invoices)
			xmlContent, err := j.openAPIClient.DownloadInvoiceXML(ctx, inv.UUID)
			if err != nil {
				j.logger.Error("failed to download invoice XML",
					"uuid", inv.UUID,
					"error", err,
				)
				continue
			}

			// Fall back to CreatedAt if invoice date not found in payload
			if invoiceDate.IsZero() {
				if !inv.CreatedAt.IsZero() {
					invoiceDate = inv.CreatedAt
				} else {
					invoiceDate = time.Now()
				}
			}

			// Create the invoice record
			invoice := &models.Invoice{
				UUID:          uuid.New().String(),
				OpenAPIUUID:   inv.UUID,
				SDIIdentifier: inv.SDIFileID,
				Direction:     direction,
				DocumentType:  docType,
				Number:        invoiceNumber,
				Date:          invoiceDate,
				Currency:      "EUR",
				TotalAmount:   totalAmount,
				Status:        status,
				SDIStatus:     sdiStatus,
				XMLContent:    string(xmlContent),
				CedentePrestatore: &models.PartyData{
					FiscalIDCode: cedenteFiscalID,
					Denomination: cedenteName,
					IsCompany:    cedenteIsCompany,
				},
				CessionarioCommittente: &models.PartyData{
					FiscalIDCode: cessionarioFiscalID,
					Denomination: cessionarioName,
					IsCompany:    cessionarioIsCompany,
				},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			if err := j.invoiceRepo.Create(ctx, invoice); err != nil {
				j.logger.Error("failed to create invoice",
					"uuid", inv.UUID,
					"direction", direction,
					"error", err,
				)
				continue
			}

			if direction == models.DirectionIssued {
				totalImportedIssued++
				j.logger.Info("imported issued invoice",
					"uuid", invoice.UUID,
					"openApiUUID", inv.UUID,
					"number", invoiceNumber,
					"cedente", cedenteName,
					"cessionario", cessionarioName,
					"amount", totalAmount,
					"docType", docType,
				)
			} else {
				totalImportedReceived++
				j.logger.Info("imported received invoice",
					"uuid", invoice.UUID,
					"openApiUUID", inv.UUID,
					"number", invoiceNumber,
					"supplier", cedenteName,
					"amount", totalAmount,
					"docType", docType,
				)
			}
		}

		// Check if there are more pages
		if page >= invoices.TotalPages || len(invoices.Invoices) < pageSize {
			break
		}
		page++
	}

	j.logger.Info("invoice sync completed",
		"totalImportedIssued", totalImportedIssued,
		"totalImportedReceived", totalImportedReceived,
		"totalSkipped", totalSkipped,
	)

	return nil
}

func (j *PollingJob) poll(ctx context.Context) error {
	j.logger.Debug("polling for SDI notifications")

	// Also sync received invoices
	if err := j.SyncReceivedInvoices(ctx); err != nil {
		j.logger.Error("failed to sync received invoices", "error", err)
		// Continue with notification polling
	}

	// Get last polling state
	state, err := j.notificationRepo.GetPollingState(ctx)
	if err != nil {
		j.logger.Error("failed to get polling state", "error", err)
		return err
	}

	// Fetch notifications from OpenAPI
	fromDate := state.LastPolledAt.Add(-1 * time.Hour) // Overlap by 1 hour to avoid missing notifications
	notifications, err := j.openAPIClient.GetNotifications(ctx, fromDate)
	if err != nil {
		j.logger.Error("failed to fetch notifications from OpenAPI", "error", err)

		// Update state with error
		state.LastError = err.Error()
		now := time.Now()
		state.LastErrorAt = &now
		state.ConsecutiveErrors++
		_ = j.notificationRepo.UpdatePollingState(ctx, state)

		return err
	}

	j.logger.Info("fetched notifications from OpenAPI",
		"count", len(notifications),
		"fromDate", fromDate.Format(time.RFC3339),
	)

	// Process each notification
	var lastNotificationTime time.Time
	processedCount := 0

	for _, n := range notifications {
		if err := j.ProcessNotification(ctx, &n); err != nil {
			j.logger.Error("failed to process notification",
				"uuid", n.UUID,
				"type", n.Type,
				"error", err,
			)
			continue
		}
		processedCount++

		if n.Date.After(lastNotificationTime) {
			lastNotificationTime = n.Date
		}
	}

	// Update polling state
	state.LastPolledAt = time.Now()
	if !lastNotificationTime.IsZero() {
		state.LastNotificationAt = &lastNotificationTime
	}
	state.TotalPolled += int64(processedCount)
	state.LastError = ""
	state.LastErrorAt = nil
	state.ConsecutiveErrors = 0

	if err := j.notificationRepo.UpdatePollingState(ctx, state); err != nil {
		j.logger.Error("failed to update polling state", "error", err)
	}

	j.logger.Info("polling completed",
		"processedCount", processedCount,
		"totalNotifications", len(notifications),
	)

	return nil
}

// ProcessNotification processes a single SDI notification, updating invoice status accordingly
func (j *PollingJob) ProcessNotification(ctx context.Context, n *services.OpenAPINotification) error {
	// Invalidate the invoice status cache before processing
	// This ensures any subsequent status queries fetch fresh data from SDI
	if err := j.openAPIClient.InvalidateInvoiceStatusCache(ctx, n.InvoiceUUID); err != nil {
		j.logger.Warn("failed to invalidate invoice status cache",
			"invoiceUUID", n.InvoiceUUID,
			"error", err,
		)
	}

	// Find the associated invoice
	invoice, err := j.invoiceRepo.GetByOpenAPIUUID(ctx, n.InvoiceUUID)
	if err != nil {
		// Invoice not found in our system - might be from a different source
		j.logger.Warn("invoice not found for notification",
			"notificationUUID", n.UUID,
			"invoiceUUID", n.InvoiceUUID,
		)
		// Still save the notification for audit purposes
		return j.SaveNotification(ctx, n, "")
	}

	// Update invoice status based on notification type
	newStatus := invoice.Status
	newSDIStatus := models.SDIStatus(n.Type)

	switch models.NotificationType(n.Type) {
	case models.NotificationRC: // Ricevuta di Consegna
		newStatus = models.StatusDelivered
		j.logger.Info("invoice delivered",
			"invoiceUUID", invoice.UUID,
			"number", invoice.Number,
		)

	case models.NotificationNS: // Notifica di Scarto
		newStatus = models.StatusRejected
		j.logger.Warn("invoice rejected by SDI",
			"invoiceUUID", invoice.UUID,
			"number", invoice.Number,
			"errorCode", n.ErrorCode,
			"errorDescription", n.ErrorDescription,
		)

	case models.NotificationMC: // Mancata Consegna
		// Status remains Sent, but SDI status changes
		j.logger.Warn("invoice delivery failed",
			"invoiceUUID", invoice.UUID,
			"number", invoice.Number,
		)

	case models.NotificationNE: // Notifica Esito (PA only)
		if n.Outcome == string(models.OutcomeAccepted) {
			newStatus = models.StatusAccepted
			j.logger.Info("invoice accepted by PA",
				"invoiceUUID", invoice.UUID,
				"number", invoice.Number,
			)
		} else {
			newStatus = models.StatusRejected
			j.logger.Warn("invoice rejected by PA",
				"invoiceUUID", invoice.UUID,
				"number", invoice.Number,
				"outcome", n.Outcome,
			)
		}

	case models.NotificationDT: // Decorrenza Termini
		newStatus = models.StatusAccepted // Silenzio-assenso
		j.logger.Info("invoice accepted by silence (decorrenza termini)",
			"invoiceUUID", invoice.UUID,
			"number", invoice.Number,
		)

	case models.NotificationAT: // Attestazione
		j.logger.Info("invoice transmission attested",
			"invoiceUUID", invoice.UUID,
			"number", invoice.Number,
		)

		// Fetch invoice status to get preserved document info for legal storage
		status, err := j.openAPIClient.GetInvoiceStatus(ctx, n.InvoiceUUID)
		if err != nil {
			j.logger.Warn("failed to get invoice status for preserved document",
				"invoiceUUID", invoice.UUID,
				"openAPIUUID", n.InvoiceUUID,
				"error", err,
			)
		} else if status.PreservedDocument != nil && status.PreservedDocument.UUID != "" {
			invoice.PreservedDocumentID = status.PreservedDocument.UUID
			j.logger.Info("captured preserved document ID",
				"invoiceUUID", invoice.UUID,
				"preservedDocumentID", status.PreservedDocument.UUID,
				"preservationStatus", status.PreservedDocument.Status,
			)
			// Save the PreservedDocumentID to the database
			if err := j.invoiceRepo.Update(ctx, invoice); err != nil {
				j.logger.Error("failed to update invoice with preserved document ID",
					"invoiceUUID", invoice.UUID,
					"preservedDocumentID", invoice.PreservedDocumentID,
					"error", err,
				)
			}
		}
	}

	// Update invoice status
	if err := j.invoiceRepo.UpdateStatus(ctx, invoice.UUID, newStatus, newSDIStatus); err != nil {
		j.logger.Error("failed to update invoice status",
			"invoiceUUID", invoice.UUID,
			"error", err,
		)
		return err
	}

	// Save the notification
	return j.SaveNotification(ctx, n, invoice.UUID)
}

// SaveNotification persists an SDI notification to the database
func (j *PollingJob) SaveNotification(ctx context.Context, n *services.OpenAPINotification, invoiceUUID string) error {
	notification := &models.SDINotification{
		UUID:             uuid.New().String(),
		InvoiceUUID:      invoiceUUID,
		OpenAPIUUID:      n.UUID,
		NotificationType: models.NotificationType(n.Type),
		NotificationDate: n.Date,
		RawContent:       n.RawContent,
		ErrorCode:        n.ErrorCode,
		ErrorDescription: n.ErrorDescription,
		Outcome:          models.NEOutcome(n.Outcome),
		Processed:        false,
		CreatedAt:        time.Now(),
	}

	return j.notificationRepo.Create(ctx, notification)
}
