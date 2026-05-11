export type ProviderName = 'stripe' | 'paypal';

export type TransactionStatus =
  | 'pending'
  | 'requires_action'
  | 'succeeded'
  | 'failed'
  | 'refunded'
  | 'partially_refunded';

export interface PaymentTransaction {
  uuid: string;
  provider: ProviderName;
  providerTxID: string;
  subscriptionUUID?: string;
  invoiceUUID?: string;
  tenantUUID?: string;
  amountCents: number;
  currency: string;
  status: TransactionStatus;
  failureCode?: string;
  failureMsg?: string;
  refundedCents?: number;
  refundedAt?: string;
  chargedAt?: string;
  description?: string;
  metadata?: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

export interface PaymentMethodRec {
  uuid: string;
  tenantUUID: string;
  provider: ProviderName;
  providerMethodID: string;
  brand?: string;
  last4?: string;
  expiryMonth?: number;
  expiryYear?: number;
  isDefault: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface PaymentWebhookEvent {
  uuid: string;
  provider: ProviderName;
  providerEventID: string;
  type: string;
  normalized?: string;
  invoiceUUID?: string;
  subscriptionUUID?: string;
  processed: boolean;
  processError?: string;
  receivedAt: string;
  processedAt?: string;
}

export interface PaymentsListResponse<T> {
  items: T[];
  total: number;
}

export interface RefundInput {
  amountCents: number;
  reason?: string;
}
