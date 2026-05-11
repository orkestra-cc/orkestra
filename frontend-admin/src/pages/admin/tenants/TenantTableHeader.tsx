import { Button, Col, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface Props {
  searchTerm: string;
  onSearchChange: (value: string) => void;
  /** True when the parent's debounced search is non-empty — exposes the
   * "include deleted users" toggle so it doesn't visually clutter the
   * toolbar when no search is in flight. */
  searchActive: boolean;
  includeDeletedUsers: boolean;
  onIncludeDeletedUsersChange: (value: boolean) => void;
  planFilter: string;
  onPlanChange: (value: string) => void;
  includeDeleted: boolean;
  onIncludeDeletedChange: (value: boolean) => void;
  onCreateClick: () => void;
  /** Heading shown at the left of the toolbar. Defaults to "Tenant
   * Management" for backwards compatibility with the legacy /admin/tenants
   * route; the Phase 3 split passes "Internal Tenants" / "Clients". */
  title?: string;
  /** Label on the "New …" button. Defaults to "New Tenant". */
  createLabel?: string;
}

const TenantTableHeader: React.FC<Props> = ({
  searchTerm,
  onSearchChange,
  searchActive,
  includeDeletedUsers,
  onIncludeDeletedUsersChange,
  planFilter,
  onPlanChange,
  onCreateClick,
  title = 'Tenant Management',
  createLabel = 'New Tenant',
}) => {
  return (
    <Row className="align-items-center g-3">
      <Col xs="auto">
        <h5 className="mb-0">{title}</h5>
      </Col>
      <Col>
        <Form.Control
          type="search"
          size="sm"
          placeholder="Search by name, slug, member email, or surname..."
          value={searchTerm}
          onChange={(e) => onSearchChange(e.target.value)}
        />
        {searchActive && (
          <Form.Check
            type="switch"
            id="tenant-search-include-deleted-users"
            label="Include soft-deleted users in member matches"
            checked={includeDeletedUsers}
            onChange={(e) => onIncludeDeletedUsersChange(e.target.checked)}
            className="fs-11 text-muted mt-1"
          />
        )}
      </Col>
      <Col xs="auto">
        <Form.Select
          size="sm"
          value={planFilter}
          onChange={(e) => onPlanChange(e.target.value)}
        >
          <option value="">All Plans</option>
          <option value="free">Free</option>
          <option value="pro">Pro</option>
          <option value="enterprise">Enterprise</option>
        </Form.Select>
      </Col>
      <Col xs="auto">
        <Button variant="primary" size="sm" onClick={onCreateClick}>
          <FontAwesomeIcon icon="plus" className="me-1" />
          {createLabel}
        </Button>
      </Col>
    </Row>
  );
};

export default TenantTableHeader;
