import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { ColumnDef } from '@tanstack/react-table';
import useAdvanceTable from './useAdvanceTable';

type Row = { id: string; name: string };

const columns: ColumnDef<Row>[] = [{ id: 'name', accessorKey: 'name' }];
const data: Row[] = [
  { id: 'r1', name: 'Ada' },
  { id: 'r2', name: 'Grace' }
];

describe('useAdvanceTable getRowId', () => {
  it('senza l opzione le righe restano identificate per indice (retrocompatibilità)', () => {
    const { result } = renderHook(() =>
      useAdvanceTable<Row>({ columns, data, selection: true })
    );
    expect(result.current.getRowModel().rows.map(r => r.id)).toEqual([
      '0',
      '1'
    ]);
  });

  it('con l opzione le righe sono identificate dal dato', () => {
    const { result } = renderHook(() =>
      useAdvanceTable<Row>({
        columns,
        data,
        selection: true,
        getRowId: row => row.id
      })
    );
    expect(result.current.getRowModel().rows.map(r => r.id)).toEqual([
      'r1',
      'r2'
    ]);
  });

  it('la selezione segue il dato quando la pagina viene riordinata', () => {
    // È il motivo per cui l opzione esiste: col polling la stessa riga può
    // cambiare posizione, e una selezione per indice si sposterebbe su
    // un altra persona.
    const { result, rerender } = renderHook(
      ({ rows }: { rows: Row[] }) =>
        useAdvanceTable<Row>({
          columns,
          data: rows,
          selection: true,
          getRowId: row => row.id,
          state: { rowSelection: { r2: true } }
        }),
      { initialProps: { rows: data } }
    );
    expect(
      result.current.getSelectedRowModel().rows.map(r => r.original.name)
    ).toEqual(['Grace']);

    rerender({ rows: [data[1], data[0]] });
    expect(
      result.current.getSelectedRowModel().rows.map(r => r.original.name)
    ).toEqual(['Grace']);
  });
});
