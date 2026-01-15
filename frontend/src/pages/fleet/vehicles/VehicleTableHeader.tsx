import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { arrayToCSV, downloadCSV, formatDateForCSV, generateTimestampedFilename } from 'utils/csvExport';
import type { VehicleResponse } from 'store/api/vehicleApi';
import AddVehicleModal from './AddVehicleModal';

const VehicleTableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedType, setSelectedType] = useState<string>('All');
  const [selectedStatus, setSelectedStatus] = useState<string>('All');
  const [showAddModal, setShowAddModal] = useState(false);

  const typeFilters = [
    'All',
    'Truck',
    'Trailer',
    'Semi-trailer',
    'Tractor',
    'Self-propelled'
  ];

  const statusFilters = [
    'All',
    'Active',
    'Inactive'
  ];

  const handleTypeFilter = (type: string) => {
    setSelectedType(type);
    applyFilters(type, selectedStatus);
  };

  const handleStatusFilter = (status: string) => {
    setSelectedStatus(status);
    applyFilters(selectedType, status);
  };

  const applyFilters = (type: string, status: string) => {
    const filters = [];

    if (type !== 'All') {
      // Map English labels back to API values
      const typeMap: Record<string, string> = {
        'Truck': 'motrice',
        'Trailer': 'rimorchio',
        'Semi-trailer': 'semi-rimorchio',
        'Tractor': 'trattore',
        'Self-propelled': 'semovente'
      };
      filters.push({ id: 'tipo', value: typeMap[type] || type.toLowerCase() });
    }

    if (status !== 'All') {
      filters.push({ id: 'isActive', value: status === 'Active' });
    }

    setColumnFilters(filters);
  };

  const handleExportCSV = () => {
    // Get filtered rows from the table
    const filteredRows = getFilteredRowModel().rows;

    // Map tipo values to English labels
    const tipoLabels: Record<string, string> = {
      motrice: 'Truck',
      rimorchio: 'Trailer',
      'semi-rimorchio': 'Semi-trailer',
      trattore: 'Tractor',
      semovente: 'Self-propelled'
    };

    // Transform data for CSV export
    const csvData = filteredRows.map((row: any) => {
      const vehicle = row.original as VehicleResponse;
      return {
        'Name': vehicle.nome,
        'License Plate': vehicle.targa,
        'Type': tipoLabels[vehicle.tipo] || vehicle.tipo,
        'Location': vehicle.luogo || '',
        'Status': vehicle.isActive ? 'Active' : 'Inactive',
        'Inspection Expiry': formatDateForCSV(vehicle.scadenzaRevisione),
        'Scheduled Inspection': formatDateForCSV(vehicle.revisioneProgrammata),
        'Notes': vehicle.note || '',
        'Created At': formatDateForCSV(vehicle.createdAt),
        'Updated At': formatDateForCSV(vehicle.updatedAt)
      };
    });

    // Define headers
    const headers = [
      'Name',
      'License Plate',
      'Type',
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
    const filename = generateTimestampedFilename('vehicles');
    downloadCSV(csv, filename);
  };

  return (
    <>
      <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Vehicle Management</h6>
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
            <span className="d-none d-sm-inline-block">Type: {selectedType}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {typeFilters.map((type) => (
              <Dropdown.Item
                key={type}
                onClick={() => handleTypeFilter(type)}
                className={selectedType === type ? 'active' : ''}
              >
                {type}
                {selectedType === type && (
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
          <div id="vehicles-actions">
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
              onClick={() => setShowAddModal(true)}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                New Vehicle
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
                  <Dropdown.Item>Usage Report</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">Delete All</Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
    </div>
      <AddVehicleModal
        show={showAddModal}
        onHide={() => setShowAddModal(false)}
      />
    </>
  );
};

export default VehicleTableHeader;