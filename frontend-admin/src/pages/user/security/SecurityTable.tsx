import { Col, Row } from 'react-bootstrap';
import { ColumnDef, Row as TableRow } from '@tanstack/react-table';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';

// SecurityTable is the AdvanceTable shell every list on /user/security renders
// through — the console's one production table primitive (DESIGN.md: "never raw
// <table> for production lists"), wired once here so the tabs contribute only
// their column definitions. Modelled on /admin/compliance's ComplianceTable.
interface SecurityTableProps<T> {
  data: T[];
  columns: ColumnDef<T>[];
  searchPlaceholder?: string;
  perPage?: number;
  // `compact` drops the search box and the footer. Sessions and trusted
  // devices grow without bound and need both; linked providers is capped at
  // the four supported IdPs, where a search box over four rows is chrome that
  // never earns its line.
  compact?: boolean;
}

// Sort a date column on its underlying timestamp.
//
// `useAdvanceTable`'s global filter matches on cell VALUES (`cell.getValue()`),
// not on rendered text, so a date column whose accessor returns the raw ISO
// string is searchable only by a string the operator can never see: on staging,
// typing the "10:0" printed in the cell matched nothing, while "08:0" — the UTC
// time behind it — returned that very row. Date columns therefore accessor the
// FORMATTED text, which makes search match what is on screen, and sort through
// this comparator so order stays chronological instead of going lexicographic
// on the formatted string ("Sep" before "Oct", "01" before "31 Dec").
export const byTimestamp =
  <T,>(pick: (row: T) => string | undefined) =>
  (a: TableRow<T>, b: TableRow<T>) => {
    const ta = new Date(pick(a.original) ?? 0).getTime() || 0;
    const tb = new Date(pick(b.original) ?? 0).getTime() || 0;
    return ta - tb;
  };

const SecurityTable = <T,>({
  data,
  columns,
  searchPlaceholder,
  perPage = 10,
  compact = false
}: SecurityTableProps<T>) => {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    pagination: !compact,
    perPage
  });

  return (
    <AdvanceTableProvider {...table}>
      {!compact && (
        <Row className="mb-3 g-2 justify-content-end">
          <Col xs="auto">
            <AdvanceTableSearchBox placeholder={searchPlaceholder} />
          </Col>
        </Row>
      )}
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          size: 'sm',
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      {!compact && (
        <div className="mt-3">
          <AdvanceTableFooter rowsPerPageSelection rowInfo navButtons />
        </div>
      )}
    </AdvanceTableProvider>
  );
};

export default SecurityTable;
