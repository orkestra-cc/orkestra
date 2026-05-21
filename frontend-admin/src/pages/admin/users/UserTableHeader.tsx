import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import {
  arrayToCSV,
  downloadCSV,
  formatDateForCSV,
  generateTimestampedFilename
} from 'utils/csvExport';
import { User } from 'store/api/userApi';
import CreateUserModal from './CreateUserModal';

const ROLE_FILTER_VALUES = [
  'super_admin',
  'administrator',
  'developer',
  'manager',
  'operator',
  'guest'
] as const;

const UserTableHeader = () => {
  const { t } = useTranslation();
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } =
    useAdvanceTableContext();
  const [selectedRole, setSelectedRole] = useState<string>('All');
  const [showCreateModal, setShowCreateModal] = useState(false);

  const roleFilters = [
    { label: t('adminUsers.tableHeader.filterAll'), value: 'All' },
    ...ROLE_FILTER_VALUES.map(value => ({
      label: t(`adminUsers.roles.${value}`),
      value
    }))
  ];

  const handleRoleFilter = (value: string) => {
    setSelectedRole(value);
    if (value === 'All') {
      setColumnFilters([]);
    } else {
      setColumnFilters([{ id: 'role', value }]);
    }
  };

  const handleExportCSV = () => {
    // Get filtered rows from the table
    const filteredRows = getFilteredRowModel().rows;

    // Map role values to localized labels
    const roleLabels: Record<string, string> = Object.fromEntries(
      ROLE_FILTER_VALUES.map(value => [value, t(`adminUsers.roles.${value}`)])
    );

    const headerFullName = t('adminUsers.tableHeader.csv.fullName');
    const headerEmail = t('adminUsers.tableHeader.csv.email');
    const headerUsername = t('adminUsers.tableHeader.csv.username');
    const headerRole = t('adminUsers.tableHeader.csv.role');
    const headerStatus = t('adminUsers.tableHeader.csv.status');
    const headerLastLogin = t('adminUsers.tableHeader.csv.lastLogin');
    const headerCreatedAt = t('adminUsers.tableHeader.csv.createdAt');
    const statusActive = t('adminUsers.tableHeader.csv.active');
    const statusInactive = t('adminUsers.tableHeader.csv.inactive');

    // Transform data for CSV export
    const csvData = filteredRows.map((row: any) => {
      const user = row.original as User;
      return {
        [headerFullName]: user.fullName,
        [headerEmail]: user.email,
        [headerUsername]: user.username,
        [headerRole]: roleLabels[user.role] || user.role,
        [headerStatus]: user.isActive ? statusActive : statusInactive,
        [headerLastLogin]: formatDateForCSV(user.lastLogin),
        [headerCreatedAt]: formatDateForCSV(user.createdAt)
      };
    });

    // Define headers
    const headers = [
      headerFullName,
      headerEmail,
      headerUsername,
      headerRole,
      headerStatus,
      headerLastLogin,
      headerCreatedAt
    ];

    // Generate CSV
    const csv = arrayToCSV(csvData, headers);

    // Download file
    const filename = generateTimestampedFilename('users');
    downloadCSV(csv, filename);
  };

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">{t('adminUsers.tableHeader.title')}</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder={t('adminUsers.tableHeader.searchPlaceholder')}
          />
        </Col>
      </Row>
      <div className="border-bottom border-200 my-3"></div>
      <div className="d-flex align-items-center justify-content-between justify-content-lg-end px-x1">
        <Dropdown className="font-sans-serif">
          <Dropdown.Toggle
            variant="orkestra-default"
            size="sm"
            className="text-600"
          >
            <FontAwesomeIcon
              icon="filter"
              transform="shrink-4"
              className="me-2"
            />
            <span className="d-none d-sm-inline-block">
              {roleFilters.find(r => r.value === selectedRole)?.label ??
                selectedRole}
            </span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {roleFilters.map(role => (
              <Dropdown.Item
                key={role.value}
                onClick={() => handleRoleFilter(role.value)}
                className={selectedRole === role.value ? 'active' : ''}
              >
                {role.label}
                {selectedRole === role.value && (
                  <FontAwesomeIcon
                    icon="check"
                    transform="down-4 shrink-4"
                    className="ms-2"
                  />
                )}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown>
        <div
          className="bg-300 mx-3 d-none d-lg-block"
          style={{ width: '1px', height: '29px' }}
        ></div>
        {getSelectedRowModel().rows.length > 0 ? (
          <div className="d-flex">
            <Form.Select
              size="sm"
              aria-label={t('adminUsers.tableHeader.bulkActionsPlaceholder')}
            >
              <option>
                {t('adminUsers.tableHeader.bulkActionsPlaceholder')}
              </option>
              <option value="activate">
                {t('adminUsers.tableHeader.bulkActivate')}
              </option>
              <option value="deactivate">
                {t('adminUsers.tableHeader.bulkDeactivate')}
              </option>
              <option value="delete">
                {t('adminUsers.tableHeader.bulkDelete')}
              </option>
            </Form.Select>
            <Button
              type="button"
              variant="orkestra-default"
              size="sm"
              className="ms-2"
            >
              {t('adminUsers.tableHeader.bulkApply')}
            </Button>
          </div>
        ) : (
          <div id="users-actions">
            <IconButton
              variant="orkestra-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
              onClick={() => setShowCreateModal(true)}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                {t('adminUsers.tableHeader.newUser')}
              </span>
            </IconButton>
            <IconButton
              variant="orkestra-default"
              size="sm"
              icon="external-link-alt"
              transform="shrink-3"
              className="mx-2"
              iconAlign="middle"
              onClick={handleExportCSV}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                {t('adminUsers.tableHeader.export')}
              </span>
            </IconButton>
            <Dropdown align="end" className="btn-reveal-trigger d-inline-block">
              <Dropdown.Toggle variant="orkestra-default" size="sm">
                <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
              </Dropdown.Toggle>

              <Dropdown.Menu className="border py-0">
                <div className="py-2">
                  <Dropdown.Item as="button" type="button">
                    {t('adminUsers.tableHeader.viewAll')}
                  </Dropdown.Item>
                  <Dropdown.Item>
                    {t('adminUsers.tableHeader.export')}
                  </Dropdown.Item>
                  <Dropdown.Item>
                    {t('adminUsers.tableHeader.import')}
                  </Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">
                    {t('adminUsers.tableHeader.deleteAll')}
                  </Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
      <CreateUserModal
        show={showCreateModal}
        onHide={() => setShowCreateModal(false)}
      />
    </div>
  );
};

export default UserTableHeader;
