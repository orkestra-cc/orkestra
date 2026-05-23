import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faSync } from '@fortawesome/free-solid-svg-icons';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { Link } from 'react-router';
import { toast } from 'react-toastify';
import { useTranslation } from 'react-i18next';
import {
  arrayToCSV,
  downloadCSV,
  formatDateForCSV,
  generateTimestampedFilename
} from 'utils/csvExport';
import type { InvoiceSummary, InvoiceStatus } from 'types/billing';
import {
  INVOICE_STATUS_LABELS,
  DOCUMENT_TYPE_LABELS,
  formatCurrency
} from 'types/billing';
import { useSyncInvoicesMutation } from 'store/api/billingApi';

const IssuedInvoiceTableHeader = () => {
  const { t } = useTranslation();
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } =
    useAdvanceTableContext();
  const [selectedStatus, setSelectedStatus] = useState<string>(
    t('billing.issued.filters.all')
  );
  const [syncInvoices, { isLoading: isSyncing }] = useSyncInvoicesMutation();

  const handleSync = async () => {
    try {
      await syncInvoices().unwrap();
      toast.success(t('billing.issued.syncSuccess'));
    } catch {
      toast.error(t('billing.issued.syncError'));
    }
  };

  const statusFilters: { label: string; value: InvoiceStatus | 'all' }[] = [
    { label: t('billing.issued.filters.all'), value: 'all' },
    { label: t('billing.issued.filters.draft'), value: 'draft' },
    { label: t('billing.issued.filters.pending'), value: 'pending' },
    { label: t('billing.issued.filters.sent'), value: 'sent' },
    { label: t('billing.issued.filters.delivered'), value: 'delivered' },
    { label: t('billing.issued.filters.rejected'), value: 'rejected' },
    { label: t('billing.issued.filters.accepted'), value: 'accepted' },
    { label: t('billing.issued.filters.paid'), value: 'paid' }
  ];

  const handleStatusFilter = (filter: {
    label: string;
    value: InvoiceStatus | 'all';
  }) => {
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
        Numero: invoice.number,
        'Tipo Documento':
          DOCUMENT_TYPE_LABELS[invoice.documentType] || invoice.documentType,
        Data: formatDateForCSV(invoice.date),
        Cliente: invoice.partyName,
        Importo: formatCurrency(invoice.totalAmount).replace('€', '').trim(),
        Stato: INVOICE_STATUS_LABELS[invoice.status],
        'SDI ID': invoice.sdiIdentifier || '',
        'Creato il': formatDateForCSV(invoice.createdAt)
      };
    });

    const headers = [
      'Numero',
      'Tipo Documento',
      'Data',
      'Cliente',
      'Importo',
      'Stato',
      'SDI ID',
      'Creato il'
    ];

    const csv = arrayToCSV(csvData, headers);
    const filename = generateTimestampedFilename('fatture_emesse');
    downloadCSV(csv, filename);
  };

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">{t('billing.issued.tableTitle')}</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder={t('billing.issued.searchPlaceholder')}
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
            <span className="d-none d-sm-inline-block">{selectedStatus}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {statusFilters.map(filter => (
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
            <Form.Select size="sm" aria-label={t('billing.issued.bulkActions')}>
              <option>{t('billing.issued.bulkActions')}</option>
              <option value="send">{t('billing.issued.bulkSend')}</option>
              <option value="delete">{t('billing.issued.bulkDelete')}</option>
            </Form.Select>
            <Button
              type="button"
              variant="orkestra-default"
              size="sm"
              className="ms-2"
            >
              {t('billing.issued.apply')}
            </Button>
          </div>
        ) : (
          <div id="invoice-actions">
            <IconButton
              as={Link}
              to="/billing/invoices/issued/new"
              variant="orkestra-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                {t('billing.issued.newInvoice')}
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
                {t('billing.issued.export')}
              </span>
            </IconButton>
            <Dropdown align="end" className="btn-reveal-trigger d-inline-block">
              <Dropdown.Toggle variant="orkestra-default" size="sm">
                <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
              </Dropdown.Toggle>

              <Dropdown.Menu className="border py-0">
                <div className="py-2">
                  <Dropdown.Item as={Link} to="/billing/invoices/issued">
                    {t('billing.issued.viewAll')}
                  </Dropdown.Item>
                  <Dropdown.Item>{t('billing.issued.exportXml')}</Dropdown.Item>
                  <Dropdown.Item>
                    {t('billing.issued.importFromXml')}
                  </Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item onClick={handleSync} disabled={isSyncing}>
                    <FontAwesomeIcon
                      icon={faSync}
                      className={`me-2 ${isSyncing ? 'fa-spin' : ''}`}
                    />
                    {t('billing.issued.syncWithSdi')}
                  </Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">
                    {t('billing.issued.deleteSelected')}
                  </Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
    </div>
  );
};

export default IssuedInvoiceTableHeader;
