import {
  useReactTable,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  ColumnDef,
  Row,
  Table,
  TableState,
  ColumnFiltersState
} from '@tanstack/react-table';
import IndeterminateCheckbox from 'components/common/advance-table/IndeterminateCheckbox';

const selectionColumn = <T,>(selectionColumnWidth?: string | number, selectionHeaderClassname?: string): ColumnDef<T> => {
  return {
    id: 'selection',
    accessorKey: '',
    header: ({ table }: { table: Table<T> }) => (
      <IndeterminateCheckbox
        className="form-check mb-0"
        {...{
          checked: table.getIsAllRowsSelected(),
          indeterminate: table.getIsSomeRowsSelected(),
          onChange: table.getToggleAllRowsSelectedHandler()
        }}
      />
    ),
    cell: ({ row }: { row: Row<T> }) => (
      <IndeterminateCheckbox
        className="form-check mb-0"
        {...{
          checked: row.getIsSelected(),
          disabled: !row.getCanSelect(),
          indeterminate: row.getIsSomeSelected(),
          onChange: row.getToggleSelectedHandler()
        }}
      />
    ),
    meta: {
      headerProps: {
        className: selectionHeaderClassname,
        style: {
          width: selectionColumnWidth
        }
      },
      cellProps: {
        style: {
          width: selectionColumnWidth
        }
      }
    }
  };
};

interface UseAdvanceTableOptions<T> {
  columns: ColumnDef<T>[];
  data: T[];
  sortable?: boolean;
  selection?: boolean;
  selectionColumnWidth?: string | number;
  selectionHeaderClassname?: string;
  pagination?: boolean;
  initialState?: Partial<TableState>;
  perPage?: number;
}

const useAdvanceTable = <T,>({
  columns,
  data,
  sortable,
  selection,
  selectionColumnWidth,
  selectionHeaderClassname,
  pagination,
  initialState,
  perPage = 10
}: UseAdvanceTableOptions<T>) => {
  const state: Partial<TableState> = {
    pagination: { pageSize: pagination ? perPage : data.length, pageIndex: 0 },
    columnFilters: [] as ColumnFiltersState,
    ...initialState
  };

  // Custom global filter function for better search
  const globalFilterFn = (row: Row<T>, _columnId: string, filterValue: string) => {
    const search = filterValue.toLowerCase();

    // Get all row values
    const rowValues = row.getAllCells().map(cell => {
      const value = cell.getValue();
      return value ? String(value).toLowerCase() : '';
    });

    // Check if any value contains the search term
    return rowValues.some(value => value.includes(search));
  };

  const table = useReactTable({
    data,
    columns: selection
      ? [
          selectionColumn<T>(selectionColumnWidth, selectionHeaderClassname),
          ...columns
        ]
      : columns,
    enableSorting: sortable,
    enableColumnFilters: true,
    enableGlobalFilter: true,
    globalFilterFn: globalFilterFn as any,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: state,
    autoResetPageIndex: false
  });

  return table;
};

export default useAdvanceTable;
