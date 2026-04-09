package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// SDINotification represents a notification received from SDI
type SDINotification struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	// Unique identifier
	UUID string `bson:"uuid" json:"id" validate:"required"`

	// Related invoice
	InvoiceUUID  string `bson:"invoiceUuid" json:"invoiceUuid" validate:"required"`
	OpenAPIUUID  string `bson:"openApiUuid" json:"openApiUuid"`

	// Notification type
	NotificationType NotificationType `bson:"notificationType" json:"notificationType" validate:"required"`
	NotificationDate time.Time        `bson:"notificationDate" json:"notificationDate"`

	// SDI identifiers
	SDIIdentifier    string `bson:"sdiIdentifier,omitempty" json:"sdiIdentifier,omitempty"`
	ProgressivoInvio string `bson:"progressivoInvio,omitempty" json:"progressivoInvio,omitempty"`

	// Content
	RawContent       string `bson:"rawContent,omitempty" json:"-"` // Full XML/JSON content (not exposed)
	Description      string `bson:"description,omitempty" json:"description,omitempty"`

	// Error details (for NS - Notifica Scarto)
	ErrorCode        string `bson:"errorCode,omitempty" json:"errorCode,omitempty"`
	ErrorDescription string `bson:"errorDescription,omitempty" json:"errorDescription,omitempty"`
	ErrorSuggestion  string `bson:"errorSuggestion,omitempty" json:"errorSuggestion,omitempty"`

	// Outcome (for NE - Notifica Esito, only for PA)
	Outcome          NEOutcome `bson:"outcome,omitempty" json:"outcome,omitempty"` // EC01 (accepted) or EC02 (rejected)
	OutcomeReason    string    `bson:"outcomeReason,omitempty" json:"outcomeReason,omitempty"`

	// Mancata Consegna details (for MC)
	MCDescription    string `bson:"mcDescription,omitempty" json:"mcDescription,omitempty"`
	NextAttemptDate  *time.Time `bson:"nextAttemptDate,omitempty" json:"nextAttemptDate,omitempty"`

	// Legal storage receipt (for AT)
	PreservedDocumentID string `bson:"preservedDocumentId,omitempty" json:"preservedDocumentId,omitempty"`

	// Processing status
	Processed   bool       `bson:"processed" json:"processed"`
	ProcessedAt *time.Time `bson:"processedAt,omitempty" json:"processedAt,omitempty"`
	ProcessedBy string     `bson:"processedBy,omitempty" json:"processedBy,omitempty"`

	// Audit
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// GetStatusDescription returns a human-readable description of the notification
func (n *SDINotification) GetStatusDescription() string {
	switch n.NotificationType {
	case NotificationRC:
		return "Fattura consegnata al destinatario"
	case NotificationNS:
		return "Fattura scartata dal Sistema di Interscambio: " + n.ErrorDescription
	case NotificationMC:
		return "Mancata consegna: " + n.MCDescription
	case NotificationNE:
		if n.Outcome == OutcomeAccepted {
			return "Fattura accettata dalla Pubblica Amministrazione"
		}
		return "Fattura rifiutata dalla Pubblica Amministrazione: " + n.OutcomeReason
	case NotificationDT:
		return "Decorrenza termini: la fattura si considera accettata per silenzio-assenso"
	case NotificationAT:
		return "Attestazione di avvenuta trasmissione con impossibilità di recapito"
	default:
		return "Notifica ricevuta"
	}
}

// IsPositive returns true if this notification indicates a positive outcome
func (n *SDINotification) IsPositive() bool {
	switch n.NotificationType {
	case NotificationRC, NotificationDT:
		return true
	case NotificationNE:
		return n.Outcome == OutcomeAccepted
	default:
		return false
	}
}

// IsNegative returns true if this notification indicates a negative outcome
func (n *SDINotification) IsNegative() bool {
	switch n.NotificationType {
	case NotificationNS:
		return true
	case NotificationNE:
		return n.Outcome == OutcomeRejected
	default:
		return false
	}
}

// RequiresAction returns true if this notification requires user action
func (n *SDINotification) RequiresAction() bool {
	switch n.NotificationType {
	case NotificationNS: // Rejected - needs correction and resubmission
		return true
	case NotificationMC: // Failed delivery - might need investigation
		return true
	case NotificationNE:
		return n.Outcome == OutcomeRejected // PA rejection needs attention
	default:
		return false
	}
}

// NotificationSummary represents a summary of notifications for dashboard display
type NotificationSummary struct {
	TotalCount       int64 `json:"totalCount"`
	UnprocessedCount int64 `json:"unprocessedCount"`
	PositiveCount    int64 `json:"positiveCount"`
	NegativeCount    int64 `json:"negativeCount"`
	PendingAction    int64 `json:"pendingAction"`
}

// NotificationFilters for querying notifications
type NotificationFilters struct {
	InvoiceUUID      string           `json:"invoiceUuid,omitempty"`
	NotificationType NotificationType `json:"notificationType,omitempty"`
	Processed        *bool            `json:"processed,omitempty"`
	FromDate         *time.Time       `json:"fromDate,omitempty"`
	ToDate           *time.Time       `json:"toDate,omitempty"`
}

// PollingState tracks the last successful polling timestamp
type PollingState struct {
	ID                primitive.ObjectID `bson:"_id,omitempty"`
	LastPolledAt      time.Time          `bson:"lastPolledAt"`
	LastNotificationAt *time.Time        `bson:"lastNotificationAt,omitempty"`
	TotalPolled       int64              `bson:"totalPolled"`
	LastError         string             `bson:"lastError,omitempty"`
	LastErrorAt       *time.Time         `bson:"lastErrorAt,omitempty"`
	ConsecutiveErrors int                `bson:"consecutiveErrors"`
	UpdatedAt         time.Time          `bson:"updatedAt"`
}
