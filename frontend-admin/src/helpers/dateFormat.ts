import i18n from '../i18n';

// The console's single date-formatting layer. Before it existed the same
// screen could show "7 Aug 2026", "06 Aug 2026, 18:31" and a US
// "8/7/2026, 3:48:14 PM", and a Go zero-time rendered as "1/1/1". Locale
// follows the operator's i18n language; sentinel/invalid values render as an
// em dash instead of leaking engineering seams.

const asValidDate = (value?: string | null): Date | null => {
  if (!value) return null;
  const d = new Date(value);
  // Go's zero time (year 1) and epoch placeholders mean "never", not a date.
  if (isNaN(d.getTime()) || d.getFullYear() < 1971) return null;
  return d;
};

// Every formatter below takes an optional IANA `timeZone`. Most timestamps in
// the console are "when did this happen to me", which is correctly read in the
// viewer's own zone — that stays the default. But some records carry a zone of
// their own and mean nothing without it: anything scheduled is scheduled in the
// zone the people attending it are in, so an operator in Milan reading a record
// scheduled in Tokyo must see Tokyo's wall clock, not their own. Those call
// sites pass the record's zone.
//
// Intl throws a RangeError on a zone it cannot resolve, which would blank the
// whole cell (or, on a submit path, reject the handler's promise). A zone the
// browser rejects is a data problem, not a reason to show nothing: fall back to
// formatting in the viewer's zone rather than losing the date entirely.
const withZone = (
  opts: Intl.DateTimeFormatOptions,
  timeZone?: string
): Intl.DateTimeFormatOptions => (timeZone ? { ...opts, timeZone } : opts);

const safeFormat = (
  d: Date,
  opts: Intl.DateTimeFormatOptions,
  timeZone?: string
): string => {
  try {
    return d.toLocaleString(i18n.language, withZone(opts, timeZone));
  } catch {
    return d.toLocaleString(i18n.language, opts);
  }
};

const DATE_OPTS: Intl.DateTimeFormatOptions = {
  day: '2-digit',
  month: 'short',
  year: 'numeric'
};

const TIME_OPTS: Intl.DateTimeFormatOptions = {
  hour: '2-digit',
  minute: '2-digit'
};

/** "07 ago 2026" / "07 Aug 2026" — dates without a time component. */
export const formatDate = (
  value?: string | null,
  timeZone?: string
): string => {
  const d = asValidDate(value);
  return d ? safeFormat(d, DATE_OPTS, timeZone) : '—';
};

/** "07 ago 2026, 15:48" — timestamps where the moment matters. */
export const formatDateTime = (
  value?: string | null,
  timeZone?: string
): string => {
  const d = asValidDate(value);
  return d ? safeFormat(d, { ...DATE_OPTS, ...TIME_OPTS }, timeZone) : '—';
};

/** "15:48" — the time half of a two-line date cell. */
export const formatTime = (
  value?: string | null,
  timeZone?: string
): string => {
  const d = asValidDate(value);
  return d ? safeFormat(d, TIME_OPTS, timeZone) : '—';
};
