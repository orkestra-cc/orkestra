import { Button, Col, Dropdown, Form, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import { useState } from 'react';
import { arrayToCSV, downloadCSV, formatDateForCSV, generateTimestampedFilename } from 'utils/csvExport';
import type { SDINotification, NotificationType } from 'types/billing';
import { NOTIFICATION_TYPE_LABELS } from 'types/billing';

const NotificationTableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedFilter, setSelectedFilter] = useState<string>('Tutti');

  const filters: { label: string; value: NotificationType | 'all' | 'unprocessed' }[] = [
    { label: 'Tutti', value: 'all' },
    { label: 'Da Processare', value: 'unprocessed' },
    { label: 'Ricevuta Consegna (RC)', value: 'RC' },
    { label: 'Notifica Scarto (NS)', value: 'NS' },
    { label: 'Mancata Consegna (MC)', value: 'MC' },
    { label: 'Notifica Esito (NE)', value: 'NE' },
    { label: 'Decorrenza Termini (DT)', value: 'DT' },
    { label: 'Attestazione (AT)', value: 'AT' },
  ];

  const handleFilter = (filter: { label: string; value: NotificationType | 'all' | 'unprocessed' }) => {
    setSelectedFilter(filter.label);
    if (filter.value === 'all') {
      setColumnFilters([]);
    } else if (filter.value === 'unprocessed') {
      setColumnFilters([{ id: 'processed', value: false }]);
    } else {
      setColumnFilters([{ id: 'notificationType', value: filter.value }]);
    }
  };

  const handleExportCSV = () => {
    const filteredRows = getFilteredRowModel().rows;

    const csvData = filteredRows.map((row: any) => {
      const notification = row.original as SDINotification;
      return {
        'Tipo': NOTIFICATION_TYPE_LABELS[notification.notificationType],
        'Data': formatDateForCSV(notification.notificationDate),
        'ID SDI': notification.sdiIdentifier || '',
        'Progressivo': notification.progressivoInvio || '',
        'Descrizione': notification.description || '',
        'Codice Errore': notification.errorCode || '',
        'Descrizione Errore': notification.errorDescription || '',
        'Processato': notification.processed ? 'Si' : 'No',
        'Processato il': notification.processedAt ? formatDateForCSV(notification.processedAt) : '',
      };
    });

    const headers = [
      'Tipo',
      'Data',
      'ID SDI',
      'Progressivo',
      'Descrizione',
      'Codice Errore',
      'Descrizione Errore',
      'Processato',
      'Processato il',
    ];

    const csv = arrayToCSV(csvData, headers);
    const filename = generateTimestampedFilename('notifiche_sdi');
    downloadCSV(csv, filename);
  };

  return (
    <div className="d-lg-flex justify-content-between">
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Elenco Notifiche SDI</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Cerca per ID SDI o progressivo"
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
            <span className="d-none d-sm-inline-block">{selectedFilter}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {filters.map((filter) => (
              <Dropdown.Item
                key={filter.value}
                onClick={() => handleFilter(filter)}
                className={selectedFilter === filter.label ? 'active' : ''}
              >
                {filter.label}
                {selectedFilter === filter.label && (
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
              <option value="markProcessed">Marca come processato</option>
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
          <div id="notification-actions">
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
                  <Dropdown.Item>Marca tutte come lette</Dropdown.Item>
                  <Dropdown.Divider />
                  <Dropdown.Item>Sincronizza con SDI</Dropdown.Item>
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </div>
        )}
      </div>
    </div>
  );
};

export default NotificationTableHeader;
