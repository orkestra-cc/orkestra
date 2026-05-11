// Tier-2 self-service payment wrappers. Hand-typed against
// backend/internal/addons/payments/handlers/client_handler.go and
// client_routes.go.
//
// setup-mode Checkout (used by SubscribePage at cold subscribe time)
// saves a card without charging. payment-mode Checkout (used by the
// dashboard's pay-outstanding-invoice flow) charges a subscription's
// most recent pending invoice.
import { apiBaseURL } from '@/api/client';
import { getAccessToken } from '@/auth/tokenStore';

export type TransactionStatus =
  | 'pending'
  | 'requires_action'
  | 'succeeded'
  | 'failed'
  | 'refunded'
  | 'partially_refunded';

export type ProviderName = 'stripe' | 'paypal';

// Wire shape matches backend/internal/addons/payments/models/transaction.go.
export interface Transaction {
  uuid: string;
  provider: ProviderName;
  providerTxID: string;
  subscriptionUUID?: string;
  invoiceUUID?: string;
  ownerKind?: 'user' | 'tenant';
  ownerUUID?: string;
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

// Wire shape matches backend/internal/addons/payments/models/payment_method.go.
export interface PaymentMethod {
  uuid: string;
  ownerKind: 'user' | 'tenant';
  ownerUUID: string;
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

export interface CheckoutSessionResponse {
  sessionId: string;
  url: string;
  amountCents?: number;
  currency?: string;
  invoiceUuid?: string;
  invoiceNumber?: string;
}

export interface PaymentApiError extends Error {
  status: number;
  code?: string;
}

function err(message: string, status: number, code?: string): PaymentApiError {
  const e = new Error(message) as PaymentApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

async function readError(res: Response, fallback: string): Promise<PaymentApiError> {
  const body = (await res.json().catch(() => ({}))) as {
    detail?: string;
    title?: string;
    code?: string;
  };
  return err(body.detail ?? body.title ?? fallback, res.status, body.code);
}

async function authedJson(path: string, init?: RequestInit): Promise<Response> {
  const token = getAccessToken();
  return fetch(`${apiBaseURL}${path}`, {
    credentials: 'include',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
}

function buildQuery(params: Record<string, string | undefined>): string {
  const entries = Object.entries(params).filter(([, v]) => v != null && v !== '') as [
    string,
    string,
  ][];
  if (entries.length === 0) return '';
  const usp = new URLSearchParams();
  for (const [k, v] of entries) usp.set(k, v);
  return `?${usp.toString()}`;
}

interface ListResponse<T> {
  items: T[];
  total: number;
}

// --- Transactions ---

export interface ListMyTransactionsFilter {
  ownerKind?: 'user' | 'tenant';
  ownerUuid?: string;
  subscriptionUuid?: string;
  status?: TransactionStatus;
}

export async function listMyTransactions(
  filter: ListMyTransactionsFilter = {},
  signal?: AbortSignal,
): Promise<ListResponse<Transaction>> {
  const qs = buildQuery({
    ownerKind: filter.ownerKind,
    ownerUuid: filter.ownerUuid,
    subscriptionUuid: filter.subscriptionUuid,
    status: filter.status,
  });
  const res = await authedJson(`/v1/me/transactions${qs}`, {
    method: 'GET',
    signal,
  });
  if (!res.ok) throw await readError(res, 'Failed to load transactions');
  return (await res.json()) as ListResponse<Transaction>;
}

// --- Payment methods ---

export interface ListMyPaymentMethodsFilter {
  ownerKind?: 'user' | 'tenant';
  ownerUuid?: string;
}

export async function listMyPaymentMethods(
  filter: ListMyPaymentMethodsFilter = {},
  signal?: AbortSignal,
): Promise<ListResponse<PaymentMethod>> {
  const qs = buildQuery({
    ownerKind: filter.ownerKind,
    ownerUuid: filter.ownerUuid,
  });
  const res = await authedJson(`/v1/me/payment-methods${qs}`, {
    method: 'GET',
    signal,
  });
  if (!res.ok) throw await readError(res, 'Failed to load payment methods');
  return (await res.json()) as ListResponse<PaymentMethod>;
}

// --- Stripe Checkout (setup mode — save a card without charging) ---

// Owner is polymorphic per the post-onboarding refactor: tenant-owned
// when the user picks an admin-attached organization, user-owned for
// the personal default. The backend defaults `ownerKind` to "user" when
// the body omits both ownerUuid and tenantUuid, so a personal subscribe
// only needs the success/cancel URLs.
export interface CreateSetupCheckoutInput {
  ownerKind?: 'user' | 'tenant';
  ownerUuid?: string;
  tenantUuid?: string;
  successUrl: string;
  cancelUrl: string;
}

export async function createSetupCheckoutSession(
  input: CreateSetupCheckoutInput,
): Promise<CheckoutSessionResponse> {
  const res = await authedJson('/v1/me/payments/setup-checkout-session', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, 'Failed to open checkout');
  return (await res.json()) as CheckoutSessionResponse;
}

// --- Stripe Checkout (payment mode — pay an outstanding invoice) ---

export interface CreatePaymentCheckoutInput {
  subscriptionUuid: string;
  successUrl: string;
  cancelUrl: string;
}

export async function createPaymentCheckoutSession(
  input: CreatePaymentCheckoutInput,
): Promise<CheckoutSessionResponse> {
  // Backend wire field is `subscriptionUuid` (camel-cased per Huma's
  // default JSON tag rules) — see MeCreateCheckoutSessionRequest.
  const res = await authedJson('/v1/me/payments/checkout-session', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, 'Failed to open checkout');
  return (await res.json()) as CheckoutSessionResponse;
}
