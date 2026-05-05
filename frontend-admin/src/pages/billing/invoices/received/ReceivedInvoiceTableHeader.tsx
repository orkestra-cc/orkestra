import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { arrayToCSV, downloadCSV, formatDateForCSV, generateTimestampedFilename } from 'utils/csvExport';
import type { InvoiceSummary, InvoiceStatus } from 'types/billing';
import { INVOICE_STATUS_LABELS, DOCUMENT_TYPE_LABELS, formatCurrency } from 'types/billing';
import ImportXMLModal from './ImportXMLModal';

const ReceivedInvoiceTableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedStatus, setSelectedStatus] = useState<string>('Tutti');
  const [showImportModal, setShowImportModal] = useState(false);

  const statusFilters: { label: string; value: InvoiceStatus | 'all' }[] = [
    { label: 'Tutti', value: 'all' },
    { label: 'In Attesa', value: 'pending' },
    { label: 'Accettata', value: 'accepted' },
    { label: 'Rifiutata', value: 'rejected' },
    { label: 'Pagata', value: 'paid' },
  ];

  const handleStatusFilter = (filter: { label: string; value: InvoiceStatus | 'all' }) => {
    setSelectedStatus(filter.label);
    if (filter.value === 'all') {
      setColumnFilters([]);
    } else {
      setColumnFilters([{ id: 'status', value: filter.value }]);
    }
  };

  const handleExportCSV = () => {
    const filteredRows = getFilteredRowModel().rows;

    const csvData = filteredRows.map((row: any) => {
      const invoice = row.original as InvoiceSummary;
      return {
        'Numero': invoice.number,
        'Tipo Documento': DOCUMENT_TYPE_LABELS[invoice.documentType] || invoice.documentType,
        'Data': formatDateForCSV(invoice.date),
        'Fornitore': invoice.partyName,
        'Importo': formatCurrency(invoice.totalAmount).replace('€', '').trim(),
        'Stato': INVOICE_STATUS_LABELS[invoice.status],
        'SDI ID': invoice.sdiIdentifier || '',
        'Ricevuto il': formatDateForCSV(invoice.createdAt),
      };
    });

    const headers = [
      'Numero',
      'Tipo Documento',
      'Data',
      'Fornitore',
      'Importo',
      'Stato',
      'SDI ID',
      'Ricevuto il',
    ];

    const csv = arrayToCSV(csvData, headers);
    const filename = generateTimestampedFilename('fatture_ricevute');
    downloadCSV(csv, filename);
  };

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Elenco Fatture Ricevute</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Cerca per numero o fornitore"
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
            {statusFilters.map((filter) => (
              <Dropdown.Item
                key={filter.value}
                onClick={() => handleStatusFilter(filter)}
                className={selectedStatus === filter.label ? 'active' : ''}
              >
                {filter.label}
                {selectedStatus === filter.label && (
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
              <option value="accept">Accetta</option>
              <option value="reject">Rifiuta</option>
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
          <div id="invoice-actions">
            <IconButton
              variant="falcon-default"
              size="sm"
              icon="external-link-alt"
              transform="shrink-3"
              iconAlign="middle"
              onClick={handleExportCSV}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                Esporta
              </span>
            </IconButton>
            <Dropdown align="end" className="btn-reveal-trigger d-inline-block ms-2">
              <Dropdown.Toggle variant="falcon-default" size="sm">
                <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
              </Dropdown.Toggle>

              <Dropdown.Menu className="border py-0">
                <div className="py-2">
                  <Dropdown.Item>Visualizza Tutti</Dropdown.Item>
                  <Dropdown.Item>Esporta XML</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item onClick={() => setShowImportModal(true)}>
                    <FontAwesomeIcon icon="file-import" className="me-2 text-primary" />
                    Importa XML
                  </Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>

      <ImportXMLModal
        show={showImportModal}
        onHide={() => setShowImportModal(false)}
      />
    </div>
  );
};

export default ReceivedInvoiceTableHeader;
