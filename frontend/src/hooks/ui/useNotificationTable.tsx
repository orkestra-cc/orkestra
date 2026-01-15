import { useMemo, useState, useCallback } from 'react';
import { ColumnDef, createColumnHelper } from '@tanstack/react-table';
import { Link } from 'react-router';
import { Badge, Button, Modal, Dropdown, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faBell,
  faCheck,
  faTimes,
  faInfoCircle,
  faExclamationTriangle,
  faClock,
  faEye,
  faCheckCircle,
} from '@fortawesome/free-solid-svg-icons';
import { useGetNotificationsQuery, useMarkNotificationProcessedMutation } from 'store/api/billingApi';
import useAdvanceTable from './useAdvanceTable';
import type { SDINotification, NotificationType } from 'types/billing';
import { NOTIFICATION_TYPE_LABELS, formatItalianDate } from 'types/billing';

interface UseNotificationTableOptions {
  selection?: boolean;
  sortable?: boolean;
  pagination?: boolean;
  perPage?: number;
  selectionColumnWidth?: number;
}

const getNotificationIcon = (type: NotificationType) => {
  const icons: Record<NotificationType, { icon: any; color: string }> = {
    RC: { icon: faCheck, color: 'text-success' },
    NS: { icon: faTimes, color: 'text-danger' },
    MC: { icon: faInfoCircle, color: 'text-info' },
    NE: { icon: faExclamationTriangle, color: 'text-warning' },
    DT: { icon: faClock, color: 'text-secondary' },
    AT: { icon: faCheck, color: 'text-primary' },
  };
  return icons[type] || { icon: faBell, color: 'text-body-tertiary' };
};

const getNotificationBadgeVariant = (type: NotificationType): string => {
  const variants: Record<NotificationType, string> = {
    RC: 'success',
    NS: 'danger',
    MC: 'info',
    NE: 'warning',
    DT: 'secondary',
    AT: 'primary',
  };
  return variants[type] || 'secondary';
};

const useNotificationTable = ({
  selection = false,
  sortable = false,
  pagination = false,
  perPage = 10,
  selectionColumnWidth = 52
}: UseNotificationTableOptions = {}) => {
  const [notificationToProcess, setNotificationToProcess] = useState<SDINotification | null>(null);

  const { data, isLoading, error } = useGetNotificationsQuery({
    pageSize: 100,
  });

  const [markProcessed, { isLoading: isProcessing }] = useMarkNotificationProcessedMutation();

  const handleMarkProcessed = useCallback(async () => {
    if (!notificationToProcess) return;
    try {
      await markProcessed(notificationToProcess.id).unwrap();
      setNotificationToProcess(null);
    } catch (err) {
      console.error('Failed to mark notification as processed:', err);
    }
  }, [notificationToProcess, markProcessed]);

  const columnHelper = createColumnHelper<SDINotification>();

  const columns = useMemo<ColumnDef<SDINotification, any>[]>(
    () => [
      columnHelper.accessor('notificationType', {
        header: 'Tipo',
        cell: ({ getValue }) => {
          const type = getValue();
          const { icon, color } = getNotificationIcon(type);
          return (
            <div className="d-flex align-items-center">
              <FontAwesomeIcon icon={icon} className={`${color} me-2`} fixedWidth />
              <Badge bg={getNotificationBadgeVariant(type)} className="text-uppercase">
                {type}
              </Badge>
            </div>
          );
        },
        enableSorting: sortable,
        filterFn: (row, columnId, filterValue) => {
          return row.getValue(columnId) === filterValue;
        },
      }),
      columnHelper.accessor('notificationDate', {
        header: 'Data',
        cell: ({ getValue }) => formatItalianDate(getValue()),
        enableSorting: sortable,
      }),
      columnHelper.accessor('sdiIdentifier', {
        header: 'ID SDI',
        cell: ({ getValue }) => (
          <span className="font-monospace fs-10">{getValue() || '-'}</span>
        ),
        enableSorting: sortable,
      }),
      columnHelper.accessor('progressivoInvio', {
        header: 'Progressivo',
        cell: ({ getValue }) => getValue() || '-',
        enableSorting: sortable,
      }),
      columnHelper.display({
        id: 'description',
        header: 'Descrizione',
        cell: ({ row }) => {
          const notification = row.original;
          const description = notification.description ||
            notification.errorDescription ||
            notification.mcDescription ||
            NOTIFICATION_TYPE_LABELS[notification.notificationType];

          return (
            <span
              className="text-truncate d-inline-block"
              style={{ maxWidth: 250 }}
              title={description}
            >
              {description}
            </span>
          );
        },
      }),
      columnHelper.accessor('processed', {
        header: 'Stato',
        cell: ({ getValue }) => (
          getValue() ? (
            <Badge bg="success" className="fs-11">
              <FontAwesomeIcon icon={faCheckCircle} className="me-1" />
              Processato
            </Badge>
          ) : (
            <Badge bg="warning" className="fs-11">
              <FontAwesomeIcon icon={faClock} className="me-1" />
              Da gestire
            </Badge>
          )
        ),
        enableSorting: sortable,
        filterFn: (row, columnId, filterValue) => {
          return row.getValue(columnId) === filterValue;
        },
      }),
      columnHelper.display({
        id: 'actions',
        header: '',
        cell: ({ row }) => {
          const notification = row.original;

          return (
            <Dropdown align="end" className="btn-reveal-trigger">
              <Dropdown.Toggle
                variant="link"
                size="sm"
                className="text-body-tertiary btn-reveal"
              >
                <FontAwesomeIcon icon="ellipsis-h" className="fs-10" />
              </Dropdown.Toggle>
              <Dropdown.Menu className="border py-2">
                <Dropdown.Item as={Link} to={`/billing/invoices/issued?sdiId=${notification.sdiIdentifier}`}>
                  <FontAwesomeIcon icon={faEye} className="me-2" fixedWidth />
                  Vedi Fattura
                </Dropdown.Item>
                {!notification.processed && (
                  <Dropdown.Item onClick={() => setNotificationToProcess(notification)}>
                    <FontAwesomeIcon icon={faCheckCircle} className="me-2 text-success" fixedWidth />
                    Marca come processato
                  </Dropdown.Item>
                )}
                {notification.errorCode && (
                  <Dropdown.Item className="text-danger">
                    <FontAwesomeIcon icon={faInfoCircle} className="me-2" fixedWidth />
                    Dettagli Errore
                  </Dropdown.Item>
                )}
              </Dropdown.Menu>
            </Dropdown>
          );
        },
      }),
    ],
    [sortable, columnHelper]
  );

  // Mark as processed modal component
  const MarkProcessedModal = useCallback(() => (
    <Modal
      show={!!notificationToProcess}
      onHide={() => setNotificationToProcess(null)}
      centered
    >
      <Modal.Header closeButton>
        <Modal.Title>Marca come Processato</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {notificationToProcess && (
          <div>
            <p className="mb-2">
              Confermi di aver gestito la notifica{' '}
              <strong>{NOTIFICATION_TYPE_LABELS[notificationToProcess.notificationType]}</strong>?
            </p>
            <div className="bg-body-secondary p-3 rounded mb-3">
              <div className="mb-2">
                <small className="text-body-tertiary">ID SDI:</small>{' '}
                <span className="font-monospace">{notificationToProcess.sdiIdentifier || '-'}</span>
              </div>
              <div className="mb-2">
                <small className="text-body-tertiary">Data:</small>{' '}
                {formatItalianDate(notificationToProcess.notificationDate)}
              </div>
              {notificationToProcess.errorDescription && (
                <div>
                  <small className="text-body-tertiary">Errore:</small>{' '}
                  <span className="text-danger">{notificationToProcess.errorDescription}</span>
                </div>
              )}
            </div>
            <div className="bg-info-subtle p-3 rounded">
              <small className="text-info">
                <FontAwesomeIcon icon="info-circle" className="me-2" />
                La notifica verrà segnata come processata e non comparirà più negli avvisi.
              </small>
            </div>
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          onClick={() => setNotificationToProcess(null)}
          disabled={isProcessing}
        >
          Annulla
        </Button>
        <Button variant="success" onClick={handleMarkProcessed} disabled={isProcessing}>
          {isProcessing ? (
            <>
              <Spinner size="sm" className="me-2" />
              Elaborazione...
            </>
          ) : (
            <>
              <FontAwesomeIcon icon={faCheckCircle} className="me-2" />
              Conferma
            </>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  ), [notificationToProcess, isProcessing, handleMarkProcessed]);

  const table = useAdvanceTable({
    columns,
    data: data?.notifications || [],
    selection,
    sortable,
    pagination,
    perPage,
    selectionColumnWidth,
  });

  return {
    ...table,
    isLoading,
    error,
    MarkProcessedModal,
  };
};

export default useNotificationTable;
