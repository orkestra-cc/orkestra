// Date-input <-> instant conversions for legal holds. Both use the operator's
// local zone, so «expires on 5 June» means the whole of 5 June as they see it.

const parts = (day: string): [number, number, number] => {
  const [y, m, d] = day.split('-').map(Number);
  return [y, m - 1, d];
};

const pad = (n: number) => String(n).padStart(2, '0');
const localDay = (d: Date) =>
  `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

// Review dates start the chosen day (local midnight).
export const reviewDayToInstant = (day: string): string => {
  const [y, m, d] = parts(day);
  return new Date(y, m, d).toISOString();
};

// Expiry dates cover the chosen day: the instant is the next local midnight.
export const expiryDayToInstant = (day: string): string => {
  const [y, m, d] = parts(day);
  return new Date(y, m, d + 1).toISOString();
};

export const instantToReviewDay = (iso?: string): string =>
  iso && !Number.isNaN(Date.parse(iso)) ? localDay(new Date(iso)) : '';

// The last millisecond before the stored instant is still on the chosen day.
export const instantToExpiryDay = (iso?: string): string =>
  iso && !Number.isNaN(Date.parse(iso))
    ? localDay(new Date(Date.parse(iso) - 1))
    : '';

// For display through formatDate (which formats in the local zone).
export const expiryDisplayInstant = (iso: string): string =>
  new Date(Date.parse(iso) - 1).toISOString();

export const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
