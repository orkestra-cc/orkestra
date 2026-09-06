import { Col, Row } from 'react-bootstrap';
import { ColumnDef } from '@tanstack/react-table';
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
