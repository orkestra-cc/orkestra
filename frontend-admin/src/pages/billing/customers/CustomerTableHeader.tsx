import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { arrayToCSV, downloadCSV, formatDateForCSV, generateTimestampedFilename } from 'utils/csvExport';
import type { Customer } from 'types/billing';
import { getPartyDisplayName } from 'types/billing';
import CustomerModal from './CustomerModal';

const CustomerTableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedStatus, setSelectedStatus] = useState<string>('Tutti');
  const [showCreateModal, setShowCreateModal] = useState(false);

  const statusFilters = ['Tutti', 'Attivi', 'Inattivi', 'P.A.', 'Privati'];

  const handleStatusFilter = (status: string) => {
    setSelectedStatus(status);
    switch (status) {
      case 'Attivi':
        setColumnFilters([{ id: 'isActive', value: true }]);
        break;
      case 'Inattivi':
        setColumnFilters([{ id: 'isActive', value: false }]);
        break;
      case 'P.A.':
        setColumnFilters([{ id: 'isPA', value: true }]);
        break;
      case 'Privati':
        setColumnFilters([{ id: 'isPA', value: false }]);
        break;
      default:
        setColumnFilters([]);
    }
  };

  const handleExportCSV = () => {
    const filteredRows = getFilteredRowModel().rows;

    const csvData = filteredRows.map((row: any) => {
      const customer = row.original as Customer;
      return {
        'Denominazione': getPartyDisplayName(customer),
        'P.IVA': customer.fiscalIdCode,
        'Codice Fiscale': customer.codiceFiscale || '',
        'Tipo': customer.isCompany ? 'Azienda' : 'Persona fisica',
        'Indirizzo': customer.address,
        'Città': customer.city,
        'Provincia': customer.province || '',
        'CAP': customer.postalCode,
        'Email': customer.email || '',
        'PEC': customer.pec || '',
        'Telefono': customer.phone || '',
        'Codice SDI': customer.codiceDestinatario || '',
        'P.A.': customer.isPA ? 'Sì' : 'No',
        'Stato': customer.isActive ? 'Attivo' : 'Inattivo',
        'Creato il': formatDateForCSV(customer.createdAt),
      };
    });

    const headers = [
      'Denominazione',
      'P.IVA',
      'Codice Fiscale',
      'Tipo',
      'Indirizzo',
      'Città',
      'Provincia',
      'CAP',
      'Email',
      'PEC',
      'Telefono',
      'Codice SDI',
      'P.A.',
      'Stato',
      'Creato il',
    ];

    const csv = arrayToCSV(csvData, headers);
    const filename = generateTimestampedFilename('clienti');
    downloadCSV(csv, filename);
  };

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Gestione Clienti</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Cerca per nome o P.IVA"
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
            <span className="d-none d-sm-inline-block">{selectedStatus}</span>
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
            <Form.Select size="sm" aria-label="Azioni di massa">
              <option>Azioni di massa</option>
              <option value="activate">Attiva</option>
              <option value="deactivate">Disattiva</option>
              <option value="delete">Elimina</option>
            </Form.Select>
            <Button
              type="button"
              variant="falcon-default"
              size="sm"
              className="ms-2"
            >
              Applica
            </Button>
          </div>
        ) : (
          <div id="customer-actions">
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
              onClick={() => setShowCreateModal(true)}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                Nuovo Cliente
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
                Esporta
              </span>
            </IconButton>
            <Dropdown align="end" className="btn-reveal-trigger d-inline-block">
              <Dropdown.Toggle variant="falcon-default" size="sm">
                <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
              </Dropdown.Toggle>

              <Dropdown.Menu className="border py-0">
                <div className="py-2">
                  <Dropdown.Item as="button" type="button">
                    Visualizza Tutti
                  </Dropdown.Item>
                  <Dropdown.Item>Esporta</Dropdown.Item>
                  <Dropdown.Item>Importa</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">Elimina Tutti</Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
      <CustomerModal
        show={showCreateModal}
        onHide={() => setShowCreateModal(false)}
      />
    </div>
  );
};

export default CustomerTableHeader;
