// Currency + cycle formatting helpers shared by the catalog views. The
// resolved language drives Intl locale: en-US for English, it-IT for
// Italian — keeps thousands separators and currency symbols consistent
// with each market's expectations.
import type { BillingCycle } from '@/api/catalog';

export function formatPrice(amountCents: number, currency: string, language: string): string {
  const locale = language.startsWith('it') ? 'it-IT' : 'en-US';
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: currency || 'EUR',
    minimumFractionDigits: amountCents % 100 === 0 ? 0 : 2,
  }).format(amountCents / 100);
}

export function cycleKey(cycle: BillingCycle): string {
  // Maps the backend enum to an i18n key suffix.
  return `cycle.${cycle}`;
}
