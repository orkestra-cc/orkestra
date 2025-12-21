
import { Button, Col, Form, Row } from 'react-bootstrap';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';

interface DeadlineReportsHeaderProps {
  onFilterChange: (filterType: string, value: string) => void;
  entityTypeFilter: string;
  statusFilter: string;
}

const DeadlineReportsHeader: React.FC<DeadlineReportsHeaderProps> = ({
  onFilterChange,
  entityTypeFilter,
  statusFilter,
}) => {
  const { getSelectedRowModel } = useAdvanceTableContext();

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Deadline Reports</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Search by name"
          />
        </Col>
      </Row>
      <div className="border-bottom border-200 my-3"></div>
      <div className="d-flex align-items-center justify-content-between justify-content-lg-end px-x1 flex-wrap gap-2">
        {/* Entity Type Filter */}
        <Form.Select
          size="sm"
          aria-label="Filter by type"
          value={entityTypeFilter}
          onChange={(e) => onFilterChange('entityType', e.target.value)}
          style={{ width: 'auto', minWidth: '150px' }}
        >
          <option value="">All types</option>
          <option value="vehicle">Vehicles</option>
          <option value="user">Users</option>
          <option value="medical">Medical Visits</option>
        </Form.Select>

        {/* Status Filter */}
        <Form.Select
          size="sm"
          aria-label="Filter by status"
          value={statusFilter}
          onChange={(e) => onFilterChange('status', e.target.value)}
          style={{ width: 'auto', minWidth: '150px' }}
        >
          <option value="">All statuses</option>
          <option value="expired">Expired</option>
          <option value="warning">Expiring</option>
          <option value="ok">OK</option>
        </Form.Select>

        <div
          className="bg-300 mx-2 d-none d-lg-block"
          style={{ width: '1px', height: '29px' }}
        ></div>

        {getSelectedRowModel().rows.length > 0 ? (
          <div className="d-flex">
            <Form.Select size="sm" aria-label="Bulk actions">
              <option>Bulk actions</option>
              <option value="export">Export selected</option>
            </Form.Select>
            <Button
              type="button"
              variant="falcon-default"
              size="sm"
              className="ms-2"
            >
              Apply
            </Button>
          </div>
        ) : (
          <div id="deadline-actions">
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="external-link-alt"
              transform="shrink-3"
              iconAlign="middle"
            >
              <span className="d-none d-sm-inline-block ms-1">
                Export
              </span>
            </IconButton>
          </div>
        )}
      </div>
    </div>
  );
};

export default DeadlineReportsHeader;
