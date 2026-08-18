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

/** "07 ago 2026" / "07 Aug 2026" — dates without a time component. */
export const formatDate = (value?: string | null): string => {
  const d = asValidDate(value);
  return d
    ? d.toLocaleDateString(i18n.language, {
        day: '2-digit',
        month: 'short',
        year: 'numeric'
      })
    : '—';
};

/** "07 ago 2026, 15:48" — timestamps where the moment matters. */
export const formatDateTime = (value?: string | null): string => {
  const d = asValidDate(value);
  return d
    ? d.toLocaleString(i18n.language, {
        day: '2-digit',
        month: 'short',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit'
      })
    : '—';
};

/** "15:48" — the time half of a two-line date cell. */
export const formatTime = (value?: string | null): string => {
  const d = asValidDate(value);
  return d
    ? d.toLocaleTimeString(i18n.language, {
        hour: '2-digit',
        minute: '2-digit'
      })
    : '—';
};
