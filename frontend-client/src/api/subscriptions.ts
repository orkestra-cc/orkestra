// Tier-2 self-service subscription wrappers. Hand-typed against
// backend/internal/addons/subscriptions/handlers/subscription_handler.go
// (SelfSubscribeInput / SubscriptionResponse / Get / List / Cancel /
// Reactivate / ListInvoices / ListActivity) and routes.go's
// /v1/me/subscriptions mounts. Codegen will sharpen later.
import { apiBaseURL } from '@/api/client';
import { getAccessToken } from '@/auth/tokenStore';

export type SubscriptionStatus =
  | 'active'
  | 'past_due'
  | 'suspended'
  | 'cancelled'
  | 'expired';

// Wire shape matches backend/internal/addons/subscriptions/models/subscription.go.
// Polymorphic owner is exposed as ownerKind + ownerUUID; tenant-owned
// subs carry Kind="tenant", personal subs carry Kind="user".
export interface Subscription {
  uuid: string;
  ownerKind: 'user' | 'tenant';
  ownerUUID: string;
  serviceUUID: string;
  tierCode: string;
  status: SubscriptionStatus;
  startedAt: string;
  currentPeriodStart: string;
  currentPeriodEnd: string;
  nextBillingAt?: string;
  cancelledAt?: string;
  cancelAtPeriodEnd?: boolean;
}

export type InvoiceStatus =
  | 'pending'
  | 'paid'
  | 'failed'
  | 'refunded'
  | 'void'
  | 'awaiting_manual_payment';

// Wire shape matches backend/internal/addons/subscriptions/models/invoice.go.
export interface SubscriptionInvoice {
  uuid: string;
  number: string;
  subscriptionUUID: string;
  ownerKind: 'user' | 'tenant';
  ownerUUID: string;
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

// Wire shape matches backend/internal/addons/subscriptions/models/activity_log.go.
export interface ActivityLog {
  uuid: string;
  subscriptionUUID: string;
  ownerKind?: 'user' | 'tenant';
  ownerUUID?: string;
  type: ActivityType;
  actor: string;
  message: string;
  payload?: Record<string, unknown>;
  createdAt: string;
}

export interface SubscriptionApiError extends Error {
  status: number;
  code?: string;
}

function err(message: string, status: number, code?: string): SubscriptionApiError {
  const e = new Error(message) as SubscriptionApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

async function readError(res: Response, fallback: string): Promise<SubscriptionApiError> {
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

// Owner is polymorphic per the post-onboarding refactor: when ownerKind
// is omitted (or set to "user") the subscription is created on the
// calling user's personal scope; tenant-owned subscribes pass
// ownerKind:"tenant" plus a tenantUuid the caller owns. Backend default
// is "user", so a personal subscribe only needs serviceCode + tierCode.
export interface SelfSubscribeInput {
  ownerKind?: 'user' | 'tenant';
  tenantUuid?: string;
  serviceCode: string;
  tierCode: string;
}

export async function selfSubscribe(input: SelfSubscribeInput): Promise<Subscription> {
  const res = await authedJson('/v1/me/subscriptions', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, 'Failed to create subscription');
  return (await res.json()) as Subscription;
}

export async function getMySubscription(uuid: string, signal?: AbortSignal): Promise<Subscription> {
  const res = await authedJson(`/v1/me/subscriptions/${encodeURIComponent(uuid)}`, {
    method: 'GET',
    signal,
  });
  if (!res.ok) throw await readError(res, 'Failed to load subscription');
  return (await res.json()) as Subscription;
}

export interface ListMySubscriptionsFilter {
  ownerKind?: 'user' | 'tenant';
  ownerUuid?: string;
  status?: SubscriptionStatus;
}

interface ListResponse<T> {
  items: T[];
  total: number;
}

export async function listMySubscriptions(
  filter: ListMySubscriptionsFilter = {},
  signal?: AbortSignal,
): Promise<ListResponse<Subscription>> {
  const qs = buildQuery({
    ownerKind: filter.ownerKind,
    ownerUuid: filter.ownerUuid,
    status: filter.status,
  });
  const res = await authedJson(`/v1/me/subscriptions${qs}`, {
    method: 'GET',
    signal,
  });
  if (!res.ok) throw await readError(res, 'Failed to load subscriptions');
  return (await res.json()) as ListResponse<Subscription>;
}

export async function cancelMySubscription(
  uuid: string,
  atPeriodEnd: boolean,
): Promise<Subscription> {
  const res = await authedJson(`/v1/me/subscriptions/${encodeURIComponent(uuid)}/cancel`, {
    method: 'POST',
    body: JSON.stringify({ atPeriodEnd }),
  });
  if (!res.ok) throw await readError(res, 'Failed to cancel subscription');
  return (await res.json()) as Subscription;
}

export async function reactivateMySubscription(uuid: string): Promise<Subscription> {
  const res = await authedJson(`/v1/me/subscriptions/${encodeURIComponent(uuid)}/reactivate`, {
    method: 'POST',
  });
  if (!res.ok) throw await readError(res, 'Failed to reactivate subscription');
  return (await res.json()) as Subscription;
}

export async function listMyInvoices(
  subscriptionUuid: string,
  signal?: AbortSignal,
): Promise<ListResponse<SubscriptionInvoice>> {
  const res = await authedJson(
    `/v1/me/subscriptions/${encodeURIComponent(subscriptionUuid)}/invoices`,
    { method: 'GET', signal },
  );
  if (!res.ok) throw await readError(res, 'Failed to load invoices');
  return (await res.json()) as ListResponse<SubscriptionInvoice>;
}

export async function listMyActivity(
  subscriptionUuid: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<ListResponse<ActivityLog>> {
  const qs = buildQuery({ limit: limit ? String(limit) : undefined });
  const res = await authedJson(
    `/v1/me/subscriptions/${encodeURIComponent(subscriptionUuid)}/activity${qs}`,
    { method: 'GET', signal },
  );
  if (!res.ok) throw await readError(res, 'Failed to load activity');
  return (await res.json()) as ListResponse<ActivityLog>;
}
