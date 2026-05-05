// Public catalog reader. Hand-typed against the backend's
// PublicCatalogService projection in
// backend/internal/addons/subscriptions/handlers/service_handler.go —
// kept narrow so accidentally-exposed admin fields stay out of the SPA's
// type surface even when codegen is re-run. Anonymous endpoint, no auth.
import { apiBaseURL } from '@/api/client';

export type BillingCycle = 'monthly' | 'quarterly' | 'annual';

export interface PublicPricingTier {
  code: string;
  cycle: BillingCycle;
  amountCents: number;
  currency: string;
  capabilities?: string[];
}

export interface PublicCatalogService {
  code: string;
  name: string;
  category?: string;
  description?: string;
  pricingTiers: PublicPricingTier[];
  setupFeeCents?: number;
}

interface PublicCatalogResponse {
  items: PublicCatalogService[];
  total: number;
}

export async function listPublicCatalog(signal?: AbortSignal): Promise<PublicCatalogResponse> {
  const res = await fetch(`${apiBaseURL}/v1/public/catalog/services`, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal,
  });
  if (!res.ok) {
    throw new Error(`catalog.list failed: ${res.status}`);
  }
  return (await res.json()) as PublicCatalogResponse;
}

// Catalog detail by service code. The backend exposes a single list
// endpoint (`/v1/public/catalog/services` returns every active service);
// we filter client-side so the detail page renders without a second RTT
// when the user navigates from the list. If TanStack Query is mid-fetch
// for the list, the detail page falls back to a one-shot list call.
export function findServiceByCode(
  items: PublicCatalogService[] | undefined,
  code: string,
): PublicCatalogService | undefined {
  return items?.find((s) => s.code === code);
}
