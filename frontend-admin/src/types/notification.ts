// Delivery log of the notification module (GET /v1/notifications, gated by
// notification.log.read). Only the fields the page shows are typed; the API
// returns a few more (template, subject, idempotency key) the page ignores.

export type DeliveryStatus = 'queued' | 'sent' | 'failed' | 'suppressed';
export type DeliveryType = 'transactional' | 'marketing';

export interface DeliveryLogEntry {
  id: string;
  channel: string;
  type: string;
  category: string;
  recipientAddress: string;
  status: string;
  senderSlug?: string;
  /** Bounded reason: a suppression cause, or a failed send's diagnostic. */
  error?: string;
  createdAt: string;
  sentAt?: string;
}

export interface DeliveryLogPage {
  items: DeliveryLogEntry[];
  total: number;
}

export interface DeliveryLogArgs {
  status?: DeliveryStatus;
  type?: DeliveryType;
  category?: string;
  address?: string;
  skip: number;
  limit: number;
}
