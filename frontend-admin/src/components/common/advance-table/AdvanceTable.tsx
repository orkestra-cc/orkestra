import { Table, TableProps } from 'react-bootstrap';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import { flexRender, Header, Row, Cell } from '@tanstack/react-table';
import classNames from 'classnames';

interface AdvanceTableProps {
  headerClassName?: string;
  bodyClassName?: string;
  rowClassName?: string;
  tableProps?: TableProps;
}

const AdvanceTable = ({
  headerClassName,
  bodyClassName,
  rowClassName,
  tableProps
}: AdvanceTableProps) => {
  const table = useAdvanceTableContext();
  const { getRowModel, getFlatHeaders } = table;

  return (
    <div className="table-responsive scrollbar">
      <Table {...tableProps}>
        <thead className={headerClassName}>
          <tr>
            {getFlatHeaders().map((header: Header<unknown, unknown>) => {
              const canSort = header.column.getCanSort();
              const sorted = header.column.getIsSorted();
              const toggleSort = header.column.getToggleSortingHandler();
              return (
                <th
                  key={header.id}
                  scope="col"
                  {...header.column.columnDef.meta?.headerProps}
                  className={classNames(
                    'fs-10',
                    header.column.columnDef.meta?.headerProps?.className,
                    {
                      sort: canSort,
                      desc: sorted === 'desc',
                      asc: sorted === 'asc'
                    }
                  )}
                  onClick={toggleSort}
                  tabIndex={canSort ? 0 : undefined}
                  aria-sort={
                    canSort
                      ? sorted === 'asc'
                        ? 'ascending'
                        : sorted === 'desc'
                          ? 'descending'
                          : 'none'
                      : undefined
                  }
                  onKeyDown={
                    canSort
                      ? e => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleSort?.(e);
                          }
                        }
                      : undefined
                  }
                >
                  {header.isPlaceholder
                    ? null
                    : flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody className={bodyClassName}>
          {getRowModel().rows.map((row: Row<unknown>) => (
            <tr key={row.id} className={rowClassName}>
              {row.getVisibleCells().map((cell: Cell<unknown, unknown>) => (
                <td key={cell.id} {...cell.column.columnDef.meta?.cellProps}>
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
};

export default AdvanceTable;
