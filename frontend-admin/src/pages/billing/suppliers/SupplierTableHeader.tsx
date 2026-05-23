import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  arrayToCSV,
  downloadCSV,
  formatDateForCSV,
  generateTimestampedFilename
} from 'utils/csvExport';
import type { Supplier } from 'types/billing';
import { getPartyDisplayName, REGIME_FISCALE_LABELS } from 'types/billing';
import SupplierModal from './SupplierModal';

const SupplierTableHeader = () => {
  const { t } = useTranslation();
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } =
    useAdvanceTableContext();
  // statusFilters carry stable keys so the filter logic can switch on the
  // semantic value rather than the translated label.
  const statusFilters: { key: 'all' | 'active' | 'inactive'; label: string }[] =
    [
      { key: 'all', label: t('billing.suppliers.filters.all') },
      { key: 'active', label: t('billing.suppliers.filters.active') },
      { key: 'inactive', label: t('billing.suppliers.filters.inactive') }
    ];
  const [selectedStatus, setSelectedStatus] = useState<string>(
    statusFilters[0].label
  );
  const [showCreateModal, setShowCreateModal] = useState(false);

  const handleStatusFilter = (filter: { key: string; label: string }) => {
    setSelectedStatus(filter.label);
    switch (filter.key) {
      case 'active':
        setColumnFilters([{ id: 'isActive', value: true }]);
        break;
      case 'inactive':
        setColumnFilters([{ id: 'isActive', value: false }]);
        break;
      default:
        setColumnFilters([]);
    }
  };

  const handleExportCSV = () => {
    const filteredRows = getFilteredRowModel().rows;

    const csvData = filteredRows.map((row: any) => {
      const supplier = row.original as Supplier;
      return {
        Denominazione: getPartyDisplayName(supplier),
        'P.IVA': supplier.fiscalIdCode,
        'Codice Fiscale': supplier.codiceFiscale || '',
        Tipo: supplier.isCompany ? 'Azienda' : 'Persona fisica',
        'Regime Fiscale': supplier.regimeFiscale
          ? `${supplier.regimeFiscale} - ${REGIME_FISCALE_LABELS[supplier.regimeFiscale]}`
          : '',
        Indirizzo: supplier.address,
        Città: supplier.city,
        Provincia: supplier.province || '',
        CAP: supplier.postalCode,
        Email: supplier.email || '',
        PEC: supplier.pec || '',
        Telefono: supplier.phone || '',
        IBAN: supplier.iban || '',
        Stato: supplier.isActive ? 'Attivo' : 'Inattivo',
        'Creato il': formatDateForCSV(supplier.createdAt)
      };
    });

    const headers = [
      'Denominazione',
      'P.IVA',
      'Codice Fiscale',
      'Tipo',
      'Regime Fiscale',
      'Indirizzo',
      'Città',
      'Provincia',
      'CAP',
      'Email',
      'PEC',
      'Telefono',
      'IBAN',
      'Stato',
      'Creato il'
    ];

    const csv = arrayToCSV(csvData, headers);
    const filename = generateTimestampedFilename('fornitori');
    downloadCSV(csv, filename);
  };

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">{t('billing.suppliers.tableTitle')}</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder={t('billing.suppliers.searchPlaceholder')}
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
                key={filter.key}
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
            <Form.Select
              size="sm"
              aria-label={t('billing.suppliers.bulkActions')}
            >
              <option>{t('billing.suppliers.bulkActions')}</option>
              <option value="activate">
                {t('billing.suppliers.bulkActivate')}
              </option>
              <option value="deactivate">
                {t('billing.suppliers.bulkDeactivate')}
              </option>
              <option value="delete">
                {t('billing.suppliers.bulkDelete')}
              </option>
            </Form.Select>
            <Button
              type="button"
              variant="orkestra-default"
              size="sm"
              className="ms-2"
            >
              {t('billing.suppliers.apply')}
            </Button>
          </div>
        ) : (
          <div id="supplier-actions">
            <IconButton
              variant="orkestra-default"
              size="sm"
              icon="plus"
              transform="shrink-3"
              iconAlign="middle"
              onClick={() => setShowCreateModal(true)}
            >
              <span className="d-none d-sm-inline-block d-xl-none d-xxl-inline-block ms-1">
                {t('billing.suppliers.newSupplier')}
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
                {t('billing.suppliers.export')}
              </span>
            </IconButton>
            <Dropdown align="end" className="btn-reveal-trigger d-inline-block">
              <Dropdown.Toggle variant="orkestra-default" size="sm">
                <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
              </Dropdown.Toggle>

              <Dropdown.Menu className="border py-0">
                <div className="py-2">
                  <Dropdown.Item as="button" type="button">
                    {t('billing.suppliers.viewAll')}
                  </Dropdown.Item>
                  <Dropdown.Item>{t('billing.suppliers.export')}</Dropdown.Item>
                  <Dropdown.Item>{t('billing.suppliers.import')}</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item className="text-danger">
                    {t('billing.suppliers.deleteAll')}
                  </Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
      <SupplierModal
        show={showCreateModal}
        onHide={() => setShowCreateModal(false)}
      />
    </div>
  );
};

export default SupplierTableHeader;
