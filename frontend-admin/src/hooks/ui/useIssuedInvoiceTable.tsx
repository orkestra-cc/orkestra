import { useMemo, useState, useCallback } from 'react';
import { ColumnDef, createColumnHelper } from '@tanstack/react-table';
import { Link } from 'react-router';
import { Badge, Button, Modal, Dropdown, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faFileInvoice, faPaperPlane, faEye, faTrash, faFilePdf, faFileCode, faCopy } from '@fortawesome/free-solid-svg-icons';
import { useGetInvoicesQuery, useDeleteInvoiceMutation, useSendInvoiceMutation, useDuplicateInvoiceMutation, useLazyGetInvoiceXmlQuery, useLazyGetInvoicePdfQuery } from 'store/api/billingApi';
import { useNavigate } from 'react-router';
import useAdvanceTable from './useAdvanceTable';
import type { InvoiceSummary, InvoiceStatus, DocumentType } from 'types/billing';
import {
  INVOICE_STATUS_LABELS,
  DOCUMENT_TYPE_LABELS,
  formatCurrency,
  formatItalianDate,
} from 'types/billing';

interface UseIssuedInvoiceTableOptions {
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

const useIssuedInvoiceTable = ({
  selection = false,
  sortable = false,
  pagination = false,
  perPage = 10,
  selectionColumnWidth = 52
}: UseIssuedInvoiceTableOptions = {}) => {
  const navigate = useNavigate();
  const [invoiceToSend, setInvoiceToSend] = useState<InvoiceSummary | null>(null);
  const [invoiceToDelete, setInvoiceToDelete] = useState<InvoiceSummary | null>(null);
  const [invoiceToDuplicate, setInvoiceToDuplicate] = useState<InvoiceSummary | null>(null);

  const { data, isLoading, error } = useGetInvoicesQuery({
    direction: 'issued',
    pageSize: 100,
  });

  const [deleteInvoice, { isLoading: isDeleting }] = useDeleteInvoiceMutation();
  const [sendInvoice, { isLoading: isSending }] = useSendInvoiceMutation();
  const [duplicateInvoice, { isLoading: isDuplicating }] = useDuplicateInvoiceMutation();
  const [getInvoiceXml] = useLazyGetInvoiceXmlQuery();
  const [getInvoicePdf] = useLazyGetInvoicePdfQuery();

  // Download XML file
  const handleDownloadXml = useCallback(async (invoice: InvoiceSummary) => {
    try {
      const result = await getInvoiceXml(invoice.id).unwrap();
      const encoder = new TextEncoder();
      const utf8Bytes = encoder.encode(result);
      const blob = new Blob([utf8Bytes], { type: 'application/xml; charset=utf-8' });
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `fattura_${invoice.number}.xml`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
    } catch (err) {
      console.error('Failed to download XML:', err);
    }
  }, [getInvoiceXml]);

  // Download PDF file
  const handleDownloadPdf = useCallback(async (invoice: InvoiceSummary) => {
    try {
      await getInvoicePdf({ id: invoice.id, filename: `fattura_${invoice.number}.pdf` }).unwrap();
    } catch (err) {
      console.error('Failed to download PDF:', err);
    }
  }, [getInvoicePdf]);

  const handleDelete = useCallback(async () => {
    if (!invoiceToDelete) return;
    try {
      await deleteInvoice(invoiceToDelete.id).unwrap();
      setInvoiceToDelete(null);
    } catch (err) {
      console.error('Failed to delete invoice:', err);
    }
  }, [invoiceToDelete, deleteInvoice]);

  const handleSend = useCallback(async () => {
    if (!invoiceToSend) return;
    try {
      await sendInvoice(invoiceToSend.id).unwrap();
      setInvoiceToSend(null);
    } catch (err) {
      console.error('Failed to send invoice:', err);
    }
  }, [invoiceToSend, sendInvoice]);

  const handleDuplicate = useCallback(async () => {
    if (!invoiceToDuplicate) return;
    try {
      const result = await duplicateInvoice({ id: invoiceToDuplicate.id }).unwrap();
      setInvoiceToDuplicate(null);
      // Navigate to the new duplicated invoice
      navigate(`/billing/invoices/issued/${result.id}`);
    } catch (err) {
      console.error('Failed to duplicate invoice:', err);
    }
  }, [invoiceToDuplicate, duplicateInvoice, navigate]);

  const columnHelper = createColumnHelper<InvoiceSummary>();

  const columns = useMemo<ColumnDef<InvoiceSummary, any>[]>(
    () => [
      columnHelper.accessor('number', {
        header: 'Numero',
        cell: ({ row }) => (
          <Link
            to={`/billing/invoices/issued/${row.original.id}`}
            className="fw-semibold"
          >
            <FontAwesomeIcon icon={faFileInvoice} className="text-primary me-2" />
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
        header: 'Cliente',
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
      columnHelper.accessor('sdiStatus', {
        header: 'SDI',
        cell: ({ getValue }) => {
          const status = getValue();
          if (!status) return <span className="text-body-tertiary">-</span>;
          return (
            <Badge bg="body-secondary" className="text-body">
              {status}
            </Badge>
          );
        },
        enableSorting: sortable,
      }),
      columnHelper.display({
        id: 'actions',
        header: '',
        cell: ({ row }) => {
          const invoice = row.original;
          const canSend = invoice.status === 'draft';
          const canDelete = invoice.status === 'draft';

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
                <Dropdown.Item as={Link} to={`/billing/invoices/issued/${invoice.id}`}>
                  <FontAwesomeIcon icon={faEye} className="me-2" fixedWidth />
                  Visualizza
                </Dropdown.Item>
                {canSend && (
                  <Dropdown.Item onClick={() => setInvoiceToSend(invoice)}>
                    <FontAwesomeIcon icon={faPaperPlane} className="me-2" fixedWidth />
                    Invia a SDI
                  </Dropdown.Item>
                )}
                <Dropdown.Divider />
                <Dropdown.Item onClick={() => handleDownloadPdf(invoice)}>
                  <FontAwesomeIcon icon={faFilePdf} className="me-2 text-danger" fixedWidth />
                  Scarica PDF
                </Dropdown.Item>
                <Dropdown.Item onClick={() => handleDownloadXml(invoice)}>
                  <FontAwesomeIcon icon={faFileCode} className="me-2" fixedWidth />
                  Scarica XML
                </Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item onClick={() => setInvoiceToDuplicate(invoice)}>
                  <FontAwesomeIcon icon={faCopy} className="me-2" fixedWidth />
                  Duplica
                </Dropdown.Item>
                {canDelete && (
                  <Dropdown.Item
                    className="text-danger"
                    onClick={() => setInvoiceToDelete(invoice)}
                  >
                    <FontAwesomeIcon icon={faTrash} className="me-2" fixedWidth />
                    Elimina
                  </Dropdown.Item>
                )}
              </Dropdown.Menu>
            </Dropdown>
          );
        },
      }),
    ],
    [sortable, columnHelper, handleDownloadPdf, handleDownloadXml]
  );

  // Delete confirmation modal component
  const DeleteModal = useCallback(() => (
    <Modal
      show={!!invoiceToDelete}
      onHide={() => setInvoiceToDelete(null)}
      centered
    >
      <Modal.Header closeButton>
        <Modal.Title>Elimina Fattura</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {invoiceToDelete && (
          <p className="mb-0">
            Sei sicuro di voler eliminare la fattura{' '}
            <strong>{invoiceToDelete.number}</strong>?
            <br />
            <small className="text-body-tertiary">
              Questa azione non può essere annullata.
            </small>
          </p>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          onClick={() => setInvoiceToDelete(null)}
          disabled={isDeleting}
        >
          Annulla
        </Button>
        <Button variant="danger" onClick={handleDelete} disabled={isDeleting}>
          {isDeleting ? (
            <>
              <Spinner size="sm" className="me-2" />
              Eliminazione...
            </>
          ) : (
            'Elimina'
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  ), [invoiceToDelete, isDeleting, handleDelete]);

  // Send confirmation modal component
  const SendModal = useCallback(() => (
    <Modal
      show={!!invoiceToSend}
      onHide={() => setInvoiceToSend(null)}
      centered
    >
      <Modal.Header closeButton>
        <Modal.Title>Invia Fattura a SDI</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {invoiceToSend && (
          <div>
            <p className="mb-2">
              Stai per inviare la fattura <strong>{invoiceToSend.number}</strong> al Sistema di Interscambio.
            </p>
            <div className="bg-info-subtle p-3 rounded">
              <small className="text-info">
                <FontAwesomeIcon icon="info-circle" className="me-2" />
                Una volta inviata, la fattura non potrà più essere modificata.
                Assicurati che tutti i dati siano corretti.
              </small>
            </div>
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          onClick={() => setInvoiceToSend(null)}
          disabled={isSending}
        >
          Annulla
        </Button>
        <Button variant="primary" onClick={handleSend} disabled={isSending}>
          {isSending ? (
            <>
              <Spinner size="sm" className="me-2" />
              Invio in corso...
            </>
          ) : (
            <>
              <FontAwesomeIcon icon={faPaperPlane} className="me-2" />
              Invia a SDI
            </>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  ), [invoiceToSend, isSending, handleSend]);

  // Duplicate confirmation modal component
  const DuplicateModal = useCallback(() => (
    <Modal
      show={!!invoiceToDuplicate}
      onHide={() => setInvoiceToDuplicate(null)}
      centered
    >
      <Modal.Header closeButton>
        <Modal.Title>Duplica Fattura</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {invoiceToDuplicate && (
          <div>
            <p className="mb-2">
              Stai per creare una copia della fattura <strong>{invoiceToDuplicate.number}</strong>.
            </p>
            <div className="bg-info-subtle p-3 rounded">
              <small className="text-info">
                <FontAwesomeIcon icon="info-circle" className="me-2" />
                La nuova fattura sarà in stato bozza senza numero.
                Dovrai impostare il numero prima dell'invio.
              </small>
            </div>
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          onClick={() => setInvoiceToDuplicate(null)}
          disabled={isDuplicating}
        >
          Annulla
        </Button>
        <Button variant="primary" onClick={handleDuplicate} disabled={isDuplicating}>
          {isDuplicating ? (
            <>
              <Spinner size="sm" className="me-2" />
              Duplicazione...
            </>
          ) : (
            <>
              <FontAwesomeIcon icon={faCopy} className="me-2" />
              Duplica
            </>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  ), [invoiceToDuplicate, isDuplicating, handleDuplicate]);

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
    DeleteModal,
    SendModal,
    DuplicateModal,
  };
};

export default useIssuedInvoiceTable;
