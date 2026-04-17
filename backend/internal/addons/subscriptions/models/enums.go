package models

// Collection names — all prefixed per the mongo-collection-naming rule.
const (
	ServicesCollection      = "subscriptions_services"
	ClientsCollection       = "subscriptions_clients"
	SubscriptionsCollection = "subscriptions_subscriptions"
	InvoicesCollection      = "subscriptions_invoices"
	ActivityCollection      = "subscriptions_activity"
)

// BillingCycle is the recurrence of a subscription charge.
type BillingCycle string

const (
	CycleMonthly   BillingCycle = "monthly"
	CycleQuarterly BillingCycle = "quarterly"
	CycleAnnual    BillingCycle = "annual"
)

func (c BillingCycle) IsValid() bool {
	switch c {
	case CycleMonthly, CycleQuarterly, CycleAnnual:
		return true
	}
	return false
}

// SubStatus is the subscription state machine.
type SubStatus string

const (
	SubActive    SubStatus = "active"
	SubPastDue   SubStatus = "past_due"
	SubSuspended SubStatus = "suspended"
	SubCancelled SubStatus = "cancelled"
	SubExpired   SubStatus = "expired"
)

// ClientStatus filters active clients from archived ones in reports.
type ClientStatus string

const (
	ClientActive   ClientStatus = "active"
	ClientArchived ClientStatus = "archived"
)

// InvoiceStatus tracks the settlement state of a generated invoice.
type InvoiceStatus string

const (
	InvoicePending              InvoiceStatus = "pending"
	InvoicePaid                 InvoiceStatus = "paid"
	InvoiceFailed               InvoiceStatus = "failed"
	InvoiceRefunded             InvoiceStatus = "refunded"
	InvoiceVoid                 InvoiceStatus = "void"
	InvoiceAwaitingManualPayment InvoiceStatus = "awaiting_manual_payment"
)

// ActivityType enumerates the audit events logged per subscription.
type ActivityType string

const (
	ActivityCreated        ActivityType = "created"
	ActivityCharged        ActivityType = "charged"
	ActivityChargeFailed   ActivityType = "charge_failed"
	ActivityRefunded       ActivityType = "refunded"
	ActivityCancelled      ActivityType = "cancelled"
	ActivityReactivated    ActivityType = "reactivated"
	ActivitySuspended      ActivityType = "suspended"
	ActivityTierChanged    ActivityType = "tier_changed"
	ActivityInvoiceIssued  ActivityType = "invoice_issued"
	ActivityManualPayment  ActivityType = "manual_payment_required"
)

// MaxChargeFailures is the threshold after which a subscription is suspended.
const MaxChargeFailures = 3
