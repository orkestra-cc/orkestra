import { Button, Col, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';

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
  /** Heading shown at the left of the toolbar. Falls back to the localized
   * default ("Tenant Management" / "Gestione tenant") for the legacy
   * /admin/tenants route; the Phase 3 split passes "Internal Tenants" /
   * "Clients" via the wrapper. */
  title?: string;
  /** Label on the "New …" button. Falls back to the localized default
   * ("New Tenant"). */
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
  title,
  createLabel
}) => {
  const { t } = useTranslation();
  const resolvedTitle = title ?? t('adminTenants.tableHeader.defaultTitle');
  const resolvedCreateLabel =
    createLabel ?? t('adminTenants.tableHeader.defaultCreateLabel');
  return (
    <Row className="align-items-center g-3">
      <Col xs="auto">
        <h5 className="mb-0">{resolvedTitle}</h5>
      </Col>
      <Col>
        <Form.Control
          type="search"
          size="sm"
          placeholder={t('adminTenants.tableHeader.searchPlaceholder')}
          value={searchTerm}
          onChange={e => onSearchChange(e.target.value)}
        />
        {searchActive && (
          <Form.Check
            type="switch"
            id="tenant-search-include-deleted-users"
            label={t('adminTenants.tableHeader.includeDeletedUsersToggle')}
            checked={includeDeletedUsers}
            onChange={e => onIncludeDeletedUsersChange(e.target.checked)}
            className="fs-11 text-muted mt-1"
          />
        )}
      </Col>
      <Col xs="auto">
        <Form.Select
          size="sm"
          value={planFilter}
          onChange={e => onPlanChange(e.target.value)}
        >
          <option value="">{t('adminTenants.tableHeader.planAll')}</option>
          <option value="free">{t('adminTenants.tableHeader.planFree')}</option>
          <option value="pro">{t('adminTenants.tableHeader.planPro')}</option>
          <option value="enterprise">
            {t('adminTenants.tableHeader.planEnterprise')}
          </option>
        </Form.Select>
      </Col>
      <Col xs="auto">
        <Button variant="primary" size="sm" onClick={onCreateClick}>
          <FontAwesomeIcon icon="plus" className="me-1" />
          {resolvedCreateLabel}
        </Button>
      </Col>
    </Row>
  );
};

export default TenantTableHeader;
