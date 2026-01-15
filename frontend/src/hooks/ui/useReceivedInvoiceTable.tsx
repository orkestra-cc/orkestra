import { useMemo, useState, useCallback } from 'react';
import { ColumnDef, createColumnHelper } from '@tanstack/react-table';
import { Link } from 'react-router';
import { Badge, Button, Modal, Dropdown, Spinner, Form } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faFileImport, faCheck, faTimes, faEye, faDownload } from '@fortawesome/free-solid-svg-icons';
import { useGetInvoicesQuery, useAcceptInvoiceMutation, useRejectInvoiceMutation } from 'store/api/billingApi';
import useAdvanceTable from './useAdvanceTable';
import type { InvoiceSummary, InvoiceStatus, DocumentType } from 'types/billing';
import {
  INVOICE_STATUS_LABELS,
  DOCUMENT_TYPE_LABELS,
  formatCurrency,
  formatItalianDate,
} from 'types/billing';

interface UseReceivedInvoiceTableOptions {
  selection?: boolean;
  sortable?: boolean;
  pagination?: boolean;
  perPage?: number;
  selectionColumnWidth?: number;
}

const getStatusBadgeVariant = (status: InvoiceStatus): string => {
  const variants: Record<InvoiceStatus, string> = {
    draft: 'secondary',
    pending: 'warning',
    sent: 'info',
    delivered: 'primary',
    rejected: 'danger',
    accepted: 'success',
    paid: 'success',
    cancelled: 'secondary',
  };
  return variants[status] || 'secondary';
};

const useReceivedInvoiceTable = ({
  selection = false,
  sortable = false,
  pagination = false,
  perPage = 10,
  selectionColumnWidth = 52
}: UseReceivedInvoiceTableOptions = {}) => {
  const [invoiceToAccept, setInvoiceToAccept] = useState<InvoiceSummary | null>(null);
  const [invoiceToReject, setInvoiceToReject] = useState<InvoiceSummary | null>(null);
  const [rejectReason, setRejectReason] = useState('');

  const { data, isLoading, error } = useGetInvoicesQuery({
    direction: 'received',
    pageSize: 100,
  });

  const [acceptInvoice, { isLoading: isAccepting }] = useAcceptInvoiceMutation();
  const [rejectInvoice, { isLoading: isRejecting }] = useRejectInvoiceMutation();

  const handleAccept = useCallback(async () => {
    if (!invoiceToAccept) return;
    try {
      await acceptInvoice(invoiceToAccept.id).unwrap();
      setInvoiceToAccept(null);
    } catch (err) {
      console.error('Failed to accept invoice:', err);
    }
  }, [invoiceToAccept, acceptInvoice]);

  const handleReject = useCallback(async () => {
    if (!invoiceToReject) return;
    try {
      await rejectInvoice({ id: invoiceToReject.id, reason: rejectReason }).unwrap();
      setInvoiceToReject(null);
      setRejectReason('');
    } catch (err) {
      console.error('Failed to reject invoice:', err);
    }
  }, [invoiceToReject, rejectReason, rejectInvoice]);

  const columnHelper = createColumnHelper<InvoiceSummary>();

  const columns = useMemo<ColumnDef<InvoiceSummary, any>[]>(
    () => [
      columnHelper.accessor('number', {
        header: 'Numero',
        cell: ({ row }) => (
          <Link
            to={`/billing/invoices/received/${row.original.id}`}
            className="fw-semibold"
          >
            <FontAwesomeIcon icon={faFileImport} className="text-success me-2" />
            {row.original.number}
          </Link>
        ),
        enableSorting: sortable,
      }),
      columnHelper.accessor('documentType', {
        header: 'Tipo',
        cell: ({ getValue }) => {
          const docType = getValue() as DocumentType;
          return (
            <span className="text-body-tertiary fs-10">
              {DOCUMENT_TYPE_LABELS[docType] || docType}
            </span>
          );
        },
        enableSorting: sortable,
      }),
      columnHelper.accessor('date', {
        header: 'Data',
        cell: ({ getValue }) => formatItalianDate(getValue()),
        enableSorting: sortable,
      }),
      columnHelper.accessor('partyName', {
        header: 'Fornitore',
        cell: ({ getValue }) => (
          <span className="text-truncate d-inline-block" style={{ maxWidth: 180 }}>
            {getValue()}
          </span>
        ),
        enableSorting: sortable,
      }),
      columnHelper.accessor('totalAmount', {
        header: 'Importo',
        cell: ({ getValue }) => (
          <span className="fw-medium">{formatCurrency(getValue())}</span>
        ),
        enableSorting: sortable,
      }),
      columnHelper.accessor('status', {
        header: 'Stato',
        cell: ({ getValue }) => {
          const status = getValue() as InvoiceStatus;
          return (
            <Badge
              bg={getStatusBadgeVariant(status)}
              className="text-uppercase fs-11"
            >
              {INVOICE_STATUS_LABELS[status]}
            </Badge>
          );
        },
        enableSorting: sortable,
        filterFn: (row, columnId, filterValue) => {
          return row.getValue(columnId) === filterValue;
        },
      }),
      columnHelper.accessor('createdAt', {
        header: 'Ricevuta',
        cell: ({ getValue }) => (
          <span className="text-body-tertiary">{formatItalianDate(getValue())}</span>
        ),
        enableSorting: sortable,
      }),
      columnHelper.display({
        id: 'actions',
        header: '',
        cell: ({ row }) => {
          const invoice = row.original;
          const canRespond = invoice.status === 'pending';

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
                <Dropdown.Item as={Link} to={`/billing/invoices/received/${invoice.id}`}>
                  <FontAwesomeIcon icon={faEye} className="me-2" fixedWidth />
                  Visualizza
                </Dropdown.Item>
                {canRespond && (
                  <>
                    <Dropdown.Item onClick={() => setInvoiceToAccept(invoice)}>
                      <FontAwesomeIcon icon={faCheck} className="me-2 text-success" fixedWidth />
                      Accetta
                    </Dropdown.Item>
                    <Dropdown.Item onClick={() => setInvoiceToReject(invoice)}>
                      <FontAwesomeIcon icon={faTimes} className="me-2 text-danger" fixedWidth />
                      Rifiuta
                    </Dropdown.Item>
                  </>
                )}
                <Dropdown.Item>
                  <FontAwesomeIcon icon={faDownload} className="me-2" fixedWidth />
                  Scarica XML
                </Dropdown.Item>
              </Dropdown.Menu>
            </Dropdown>
          );
        },
      }),
    ],
    [sortable, columnHelper]
  );

  // Accept confirmation modal component
  const AcceptModal = useCallback(() => (
    <Modal
      show={!!invoiceToAccept}
      onHide={() => setInvoiceToAccept(null)}
      centered
    >
      <Modal.Header closeButton>
        <Modal.Title>Accetta Fattura</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {invoiceToAccept && (
          <div>
            <p className="mb-2">
              Confermi di accettare la fattura <strong>{invoiceToAccept.number}</strong>?
            </p>
            <div className="bg-success-subtle p-3 rounded">
              <small className="text-success">
                <FontAwesomeIcon icon="check-circle" className="me-2" />
                La fattura verrà registrata come accettata e sarà visibile nel ciclo passivo.
              </small>
            </div>
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          onClick={() => setInvoiceToAccept(null)}
          disabled={isAccepting}
        >
          Annulla
        </Button>
        <Button variant="success" onClick={handleAccept} disabled={isAccepting}>
          {isAccepting ? (
            <>
              <Spinner size="sm" className="me-2" />
              Elaborazione...
            </>
          ) : (
            <>
              <FontAwesomeIcon icon={faCheck} className="me-2" />
              Accetta
            </>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  ), [invoiceToAccept, isAccepting, handleAccept]);

  // Reject confirmation modal component
  const RejectModal = useCallback(() => (
    <Modal
      show={!!invoiceToReject}
      onHide={() => {
        setInvoiceToReject(null);
        setRejectReason('');
      }}
      centered
    >
      <Modal.Header closeButton>
        <Modal.Title>Rifiuta Fattura</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {invoiceToReject && (
          <div>
            <p className="mb-3">
              Stai per rifiutare la fattura <strong>{invoiceToReject.number}</strong>.
            </p>
            <Form.Group className="mb-3">
              <Form.Label>Motivo del rifiuto <span className="text-danger">*</span></Form.Label>
              <Form.Control
                as="textarea"
                rows={3}
                value={rejectReason}
                onChange={(e) => setRejectReason(e.target.value)}
                placeholder="Inserisci il motivo del rifiuto..."
              />
            </Form.Group>
            <div className="bg-warning-subtle p-3 rounded">
              <small className="text-warning">
                <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
                Il fornitore riceverà una notifica di rifiuto con il motivo specificato.
              </small>
            </div>
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          onClick={() => {
            setInvoiceToReject(null);
            setRejectReason('');
          }}
          disabled={isRejecting}
        >
          Annulla
        </Button>
        <Button
          variant="danger"
          onClick={handleReject}
          disabled={isRejecting || !rejectReason.trim()}
        >
          {isRejecting ? (
            <>
              <Spinner size="sm" className="me-2" />
              Elaborazione...
            </>
          ) : (
            <>
              <FontAwesomeIcon icon={faTimes} className="me-2" />
              Rifiuta
            </>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  ), [invoiceToReject, rejectReason, isRejecting, handleReject]);

  const table = useAdvanceTable({
    columns,
    data: data?.invoices || [],
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
    AcceptModal,
    RejectModal,
  };
};

export default useReceivedInvoiceTable;
