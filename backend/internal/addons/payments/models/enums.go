package models

const (
	TransactionsCollection   = "payments_transactions"
	PaymentMethodsCollection = "payments_payment_methods"
	WebhookEventsCollection  = "payments_webhook_events"
)

// ProviderName identifies a payment gateway implementation.
type ProviderName string

const (
	ProviderStripe ProviderName = "stripe"
	ProviderPayPal ProviderName = "paypal" // stub — not implemented in v1
)

// TransactionStatus tracks the settlement state of a provider-side charge.
type TransactionStatus string

const (
	TxPending         TransactionStatus = "pending"         // awaiting provider response
	TxRequiresAction  TransactionStatus = "requires_action" // 3DS / user action needed
	TxSucceeded       TransactionStatus = "succeeded"
	TxFailed          TransactionStatus = "failed"
	TxRefunded        TransactionStatus = "refunded"
	TxPartialRefunded TransactionStatus = "partially_refunded"
)
