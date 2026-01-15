import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { arrayToCSV, downloadCSV, formatDateForCSV, generateTimestampedFilename } from 'utils/csvExport';
import type { TachographResponse } from 'store/api/tachographApi';
import AddTachographModal from './AddTachographModal';

const TachographTableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedStatus, setSelectedStatus] = useState<string>('All');
  const [selectedRevision, setSelectedRevision] = useState<string>('All');
  const [showAddModal, setShowAddModal] = useState(false);

  const statusFilters = [
    'All',
    'Active',
    'Inactive'
  ];

  const revisionFilters = [
    { label: 'All', value: 'All' },
    { label: 'Next 7 days', value: '7' },
    { label: 'Next 30 days', value: '30' },
    { label: 'Next 60 days', value: '60' },
    { label: 'Expired', value: 'expired' }
  ];

  const handleStatusFilter = (status: string) => {
    setSelectedStatus(status);
    applyFilters(status, selectedRevision);
  };

  const handleRevisionFilter = (revision: string) => {
    setSelectedRevision(revision);
    applyFilters(selectedStatus, revision);
  };

  const applyFilters = (status: string, revision: string) => {
    const filters = [];

    if (status !== 'All') {
      filters.push({ id: 'isActive', value: status === 'Active' });
    }

    // Note: Revision filtering would need to be handled differently
    // as it's a server-side filter. This is just for UI display
    if (revision !== 'All') {
      // You might need to implement custom filtering logic here
      // or handle it through the API query parameters
    }

    setColumnFilters(filters);
  };

  const handleExportCSV = () => {
    // Get filtered rows from the table
    const filteredRows = getFilteredRowModel().rows;

    // Transform data for CSV export
    const csvData = filteredRows.map((row: any) => {
      const tachograph = row.original as TachographResponse;
      return {
        'Name': tachograph.nome,
        'License Plate': tachograph.targa,
        'Location': tachograph.luogo || '',
        'Status': tachograph.isActive ? 'Active' : 'Inactive',
        'Inspection Expiry': formatDateForCSV(tachograph.scadenzaRevisione),
        'Scheduled Inspection': formatDateForCSV(tachograph.revisioneProgrammata),
        'Notes': tachograph.note || '',
        'Created At': formatDateForCSV(tachograph.createdAt),
        'Updated At': formatDateForCSV(tachograph.updatedAt)
      };
    });

    // Define headers
    const headers = [
      'Name',
      'License Plate',
      'Location',
      'Status',
      'Inspection Expiry',
      'Scheduled Inspection',
      'Notes',
      'Created At',
      'Updated At'
    ];

    // Generate CSV
    const csv = arrayToCSV(csvData, headers);

    // Download file
    const filename = generateTimestampedFilename('tachographs');
    downloadCSV(csv, filename);
  };

  return (
    <>
      <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Tachograph Management</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Search by name/plate"
          />
        </Col>
      </Row>
      <div className="border-bottom border-200 my-3"></div>
      <div className="d-flex align-items-center justify-content-between justify-content-lg-end px-x1">
        <Dropdown className="font-sans-serif me-2">
          <Dropdown.Toggle
            variant="falcon-default"
            size="sm"
            className="text-600"
          >
            <FontAwesomeIcon icon="filter" transform="shrink-4" className="me-2" />
            <span className="d-none d-sm-inline-block">Status: {selectedStatus}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {statusFilters.map((status) => (
              <Dropdown.Item
                key={status}
                onClick={() => handleStatusFilter(status)}
                className={selectedStatus === status ? 'active' : ''}
              >
                {status}
                {selectedStatus === status && (
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
        <Dropdown className="font-sans-serif">
          <Dropdown.Toggle
            variant="falcon-default"
            size="sm"
            className="text-600"
          >
            <FontAwesomeIcon icon="calendar-check" transform="shrink-4" className="me-2" />
            <span className="d-none d-sm-inline-block">Inspection: {revisionFilters.find(f => f.value === selectedRevision)?.label || 'All'}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {revisionFilters.map((filter) => (
              <Dropdown.Item
                key={filter.value}
                onClick={() => handleRevisionFilter(filter.value)}
                className={selectedRevision === filter.value ? 'active' : ''}
              >
                {filter.label}
                {selectedRevision === filter.value && (
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
              <option value="schedule-revision">Schedule Inspection</option>
              <option value="export-selected">Export Selected</option>
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
          <div id="tachographs-actions">
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
              onClick={() => setShowAddModal(true)}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                New Tachograph
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
                  <Dropdown.Item onClick={handleExportCSV}>Export</Dropdown.Item>
                  <Dropdown.Item>Import</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item>Expiring Inspections</Dropdown.Item>
                  <Dropdown.Item>Expired Inspections</Dropdown.Item>
                  <Dropdown.Item>Compliance Report</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">Delete All</Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
    </div>
      <AddTachographModal
        show={showAddModal}
        onHide={() => setShowAddModal(false)}
      />
    </>
  );
};

export default TachographTableHeader;
