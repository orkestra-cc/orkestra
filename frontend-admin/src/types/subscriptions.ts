// TypeScript models mirroring backend/internal/addons/subscriptions/models.

export type BillingCycle = 'monthly' | 'quarterly' | 'annual';

export type SubStatus =
  | 'active'
  | 'past_due'
  | 'suspended'
  | 'cancelled'
  | 'expired';

export type InvoiceStatus =
  | 'pending'
  | 'paid'
  | 'failed'
  | 'refunded'
  | 'void'
  | 'awaiting_manual_payment';

export type ActivityType =
  | 'created'
  | 'charged'
  | 'charge_failed'
  | 'refunded'
  | 'cancelled'
  | 'reactivated'
  | 'suspended'
  | 'tier_changed'
  | 'invoice_issued'
  | 'manual_payment_required';

export interface PricingTier {
  code: string;
  cycle: BillingCycle;
  amountCents: number;
  currency: string;
}

export interface SubscriptionService {
  uuid: string;
  code: string;
  name: string;
  category: string;
  description: string;
  active: boolean;
  pricingTiers: PricingTier[];
  setupFeeCents: number;
  metadata?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
}

export interface Subscription {
  uuid: string;
  tenantUUID: string;
  serviceUUID: string;
  tierCode: string;
  status: SubStatus;
  startedAt: string;
  currentPeriodStart: string;
  currentPeriodEnd: string;
  nextBillingAt: string;
  cancelledAt?: string;
  endsAt?: string;
  cancelAtPeriodEnd: boolean;
  failedChargeCount: number;
  paymentProvider?: string;
  paymentMethodID?: string;
  metadata?: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
}

export interface SubscriptionInvoice {
  uuid: string;
  number: string;
  subscriptionUUID: string;
  tenantUUID: string;
  serviceUUID: string;
  periodStart: string;
  periodEnd: string;
  issuedAt: string;
  dueAt: string;
  subtotalCents: number;
  vatCents: number;
  totalCents: number;
  currency: string;
  status: InvoiceStatus;
  stripePaymentIntentID?: string;
  stripeRefundID?: string;
  paidAt?: string;
  failedAt?: string;
  refundedAt?: string;
  failureCode?: string;
  failureMsg?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ActivityLog {
  uuid: string;
  subscriptionUUID: string;
  tenantUUID: string;
  type: ActivityType;
  actor: string;
  message: string;
  payload?: Record<string, unknown>;
  createdAt: string;
}

export interface ListResponse<T> {
  items: T[];
  total: number;
}

export interface CreateServiceInput {
  code: string;
  name: string;
  category: string;
  description?: string;
  active: boolean;
  pricingTiers: PricingTier[];
  setupFeeCents?: number;
  metadata?: Record<string, unknown>;
}

export type UpdateServiceInput = CreateServiceInput;

export interface CreateSubscriptionInput {
  tenantUUID: string;
  serviceUUID: string;
  tierCode: string;
}
