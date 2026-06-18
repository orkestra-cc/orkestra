import { Col, Row } from 'react-bootstrap';
import { ColumnDef } from '@tanstack/react-table';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';

// ComplianceTable is the shared, searchable + paginated AdvanceTable shell every
// compliance tab renders. Keeping the provider/search/footer wiring in one place
// keeps the four tabs to just their column definitions.
interface ComplianceTableProps<T> {
  data: T[];
  columns: ColumnDef<T>[];
  searchPlaceholder?: string;
  perPage?: number;
}

const ComplianceTable = <T,>({
  data,
  columns,
  searchPlaceholder,
  perPage = 10
}: ComplianceTableProps<T>) => {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    pagination: true,
    perPage
  });

  return (
    <AdvanceTableProvider {...table}>
      <Row className="mb-3 g-2 justify-content-end">
        <Col xs="auto">
          <AdvanceTableSearchBox placeholder={searchPlaceholder} />
        </Col>
      </Row>
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          size: 'sm',
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      <div className="mt-3">
        <AdvanceTableFooter rowsPerPageSelection rowInfo navButtons />
      </div>
    </AdvanceTableProvider>
  );
};

export default ComplianceTable;
