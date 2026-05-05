// Extend @tanstack/react-table types
import '@tanstack/react-table';

declare module '@tanstack/react-table' {
  interface ColumnMeta<TData extends RowData, TValue> {
    headerProps?: React.HTMLAttributes<HTMLTableCellElement> & {
      className?: string;
    };
    cellProps?: React.HTMLAttributes<HTMLTableCellElement> & {
      className?: string;
    };
  }
}
