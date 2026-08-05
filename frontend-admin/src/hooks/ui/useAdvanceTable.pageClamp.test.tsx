// Regression guard for the out-of-range-page bug: useAdvanceTable sets
// autoResetPageIndex:false (deliberately — a sort or a filter must not yank
// the operator back to page 1), which means the page index survives a data
// change. When rows are DELETED while the operator sits on the last page,
// that same persistence can leave pageIndex past the end and render a blank
// table even though rows remain.
import { renderHook, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import type { ColumnDef } from '@tanstack/react-table';
import useAdvanceTable from './useAdvanceTable';

interface Row {
  id: number;
}
const columns: ColumnDef<Row>[] = [{ accessorKey: 'id', header: 'id' }];
const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ id: i }));

describe('useAdvanceTable page clamping', () => {
  it('keeps the page index across an ordinary data change', () => {
    const { result, rerender } = renderHook(
      ({ data }) =>
        useAdvanceTable({ data, columns, pagination: true, perPage: 10 }),
      { initialProps: { data: rows(30) } }
    );

    act(() => result.current.setPageIndex(2));
    expect(result.current.getState().pagination.pageIndex).toBe(2);

    // Same row count, different content — the operator stays put.
    rerender({ data: rows(30) });
    expect(result.current.getState().pagination.pageIndex).toBe(2);
    expect(result.current.getRowModel().rows).toHaveLength(10);
  });

  it('does not strand the operator on a blank page when rows are removed', () => {
    const { result, rerender } = renderHook(
      ({ data }) =>
        useAdvanceTable({ data, columns, pagination: true, perPage: 10 }),
      { initialProps: { data: rows(11) } }
    );

    // Page 2 (index 1) holds exactly one row.
    act(() => result.current.setPageIndex(1));
    expect(result.current.getRowModel().rows).toHaveLength(1);

    // That row is deleted: 11 -> 10 rows, so page 2 no longer exists.
    rerender({ data: rows(10) });

    // The 10 surviving rows must still be reachable, not hidden behind a
    // page index that is now past the end.
    expect(result.current.getRowModel().rows.length).toBeGreaterThan(0);
  });
});
