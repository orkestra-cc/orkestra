import { Row } from '@tanstack/react-table';

// Sort a date column on its underlying timestamp.
//
// `useAdvanceTable`'s global filter matches on cell VALUES (`cell.getValue()`),
// not on rendered text, so a date column whose accessor returns the raw ISO
// string is searchable only by a string the operator can never see: on staging,
// typing the "10:0" printed in the cell matched nothing, while "08:0" — the UTC
// time behind it — returned that very row.
//
// A date column therefore accessors the FORMATTED text, which makes search
// match what is on screen, and sorts through this comparator so order stays
// chronological instead of going lexicographic on the formatted string ("Sep"
// after "Jan" as text, before it in time).
//
//   {
//     id: 'placedAt',
//     accessorFn: h => formatDateTime(h.placedAt),
//     sortingFn: byTimestamp<LegalHold>(h => h.placedAt),
//     ...
//   }
//
// Both halves are load-bearing: the formatted accessor without this comparator
// silently reorders the column, and this comparator without the formatted
// accessor leaves the search bug in place.
export const byTimestamp =
  <T>(pick: (row: T) => string | undefined) =>
  (a: Row<T>, b: Row<T>) => {
    const ta = new Date(pick(a.original) ?? 0).getTime() || 0;
    const tb = new Date(pick(b.original) ?? 0).getTime() || 0;
    return ta - tb;
  };
