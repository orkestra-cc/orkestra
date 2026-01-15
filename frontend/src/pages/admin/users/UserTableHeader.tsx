import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { arrayToCSV, downloadCSV, formatDateForCSV, generateTimestampedFilename } from 'utils/csvExport';
import { User } from 'store/api/userApi';
import CreateUserModal from './CreateUserModal';

const UserTableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedRole, setSelectedRole] = useState<string>('All');
  const [showCreateModal, setShowCreateModal] = useState(false);

  const roleFilters = [
    'All',
    'CEO',
    'Developer',
    'Administrator',
    'Manager',
    'Operator',
    'Guest'
  ];

  const handleRoleFilter = (role: string) => {
    setSelectedRole(role);
    if (role === 'All') {
      setColumnFilters([]);
    } else {
      setColumnFilters([{ id: 'role', value: role.toLowerCase() }]);
    }
  };

  const handleExportCSV = () => {
    // Get filtered rows from the table
    const filteredRows = getFilteredRowModel().rows;

    // Map role values to English labels
    const roleLabels: Record<string, string> = {
      ceo: 'CEO',
      developer: 'Developer',
      administrator: 'Administrator',
      manager: 'Manager',
      operator: 'Operator',
      guest: 'Guest'
    };

    // Transform data for CSV export
    const csvData = filteredRows.map((row: any) => {
      const user = row.original as User;
      return {
        'Full Name': user.fullName,
        'Email': user.email,
        'Username': user.username,
        'Role': roleLabels[user.role] || user.role,
        'Status': user.isActive ? 'Active' : 'Inactive',
        'Last Login': formatDateForCSV(user.lastLogin),
        'Created At': formatDateForCSV(user.createdAt)
      };
    });

    // Define headers
    const headers = [
      'Full Name',
      'Email',
      'Username',
      'Role',
      'Status',
      'Last Login',
      'Created At'
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
          <h6 className="mb-0">User Management</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Search by name/email"
          />
        </Col>
      </Row>
      <div className="border-bottom border-200 my-3"></div>
      <div className="d-flex align-items-center justify-content-between justify-content-lg-end px-x1">
        <Dropdown className="font-sans-serif">
          <Dropdown.Toggle
            variant="falcon-default"
            size="sm"
            className="text-600"
          >
            <FontAwesomeIcon icon="filter" transform="shrink-4" className="me-2" />
            <span className="d-none d-sm-inline-block">{selectedRole}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {roleFilters.map((role) => (
              <Dropdown.Item
                key={role}
                onClick={() => handleRoleFilter(role)}
                className={selectedRole === role ? 'active' : ''}
              >
                {role}
                {selectedRole === role && (
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
            <Form.Select size="sm" aria-label="Bulk actions">
              <option>Bulk actions</option>
              <option value="activate">Activate</option>
              <option value="deactivate">Deactivate</option>
              <option value="delete">Delete</option>
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
          <div id="users-actions">
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
              onClick={() => setShowCreateModal(true)}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                New User
              </span>
            </IconButton>
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="external-link-alt"
              transform="shrink-3"
              className="mx-2"
              iconAlign="middle"
              onClick={handleExportCSV}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                Export
              </span>
            </IconButton>
            <Dropdown align="end" className="btn-reveal-trigger d-inline-block">
              <Dropdown.Toggle variant="falcon-default" size="sm">
                <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
              </Dropdown.Toggle>

              <Dropdown.Menu className="border py-0">
                <div className="py-2">
                  <Dropdown.Item as="button" type="button">
                    View All
                  </Dropdown.Item>
                  <Dropdown.Item>Export</Dropdown.Item>
                  <Dropdown.Item>Import</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">Delete All</Dropdown.Item>
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
