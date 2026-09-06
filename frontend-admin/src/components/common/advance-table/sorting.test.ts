import { describe, it, expect } from 'vitest';
import type { Row } from '@tanstack/react-table';
import { byTimestamp } from './sorting';

interface Item {
  label: string;
  when?: string | null;
}

// byTimestamp only ever sees `row.original`, so a bare object stands in for a
// TanStack Row here rather than dragging a whole table into a unit test.
const row = (original: Item) => ({ original }) as Row<Item>;

const sortWith = (items: Item[]) => {
  const cmp = byTimestamp<Item>(i => i.when ?? undefined);
  return [...items]
    .map(row)
    .sort(cmp)
    .map(r => r.original.label);
};

describe('byTimestamp', () => {
  it('orders chronologically, not lexicographically', () => {
    // "Sep …" sorts after "Jan …" as text but before it in time — the exact
    // disagreement that makes a formatted-string comparator wrong.
    expect(
      sortWith([
        { label: 'jan2027', when: '2027-01-05T10:00:00Z' },
        { label: 'sep2026', when: '2026-09-05T10:00:00Z' }
      ])
    ).toEqual(['sep2026', 'jan2027']);
  });

  it('is symmetric — reversing the input gives the same order', () => {
    const items: Item[] = [
      { label: 'a', when: '2020-01-01T00:00:00Z' },
      { label: 'b', when: '2024-06-15T12:00:00Z' },
      { label: 'c', when: '2026-12-31T23:59:00Z' }
    ];
    expect(sortWith(items)).toEqual(['a', 'b', 'c']);
    expect(sortWith([...items].reverse())).toEqual(['a', 'b', 'c']);
  });

  it('treats a missing or unparseable value as epoch, not as NaN', () => {
    // NaN would make every comparison return NaN, which Array.prototype.sort
    // reads as 0 — a comparator that silently does nothing. That is exactly
    // how pointing this helper at a non-date field (a typo TypeScript accepts,
    // since both fields are `string`) stayed invisible until a mutation run.
    // Collapsing to 0 keeps such rows at the epoch end instead.
    expect(
      sortWith([
        { label: 'real', when: '2026-09-05T10:00:00Z' },
        { label: 'missing', when: undefined },
        { label: 'garbage', when: 'pending' }
      ])[2]
    ).toBe('real');

    const cmp = byTimestamp<Item>(i => i.when ?? undefined);
    expect(
      cmp(row({ label: 'x', when: 'pending' }), row({ label: 'y', when: null }))
    ).toBe(0);
  });

  it('reads the field the caller picks, not a fixed one', () => {
    const other = byTimestamp<Item>(i => i.label);
    // Labels are not dates, so every pair collapses to 0 — the observable
    // signature of a comparator aimed at the wrong field.
    expect(
      other(
        row({ label: 'a', when: '2020-01-01T00:00:00Z' }),
        row({ label: 'b', when: '2026-01-01T00:00:00Z' })
      )
    ).toBe(0);
  });
});
