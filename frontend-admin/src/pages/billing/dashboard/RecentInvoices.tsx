import { Card, Table, Spinner, Badge } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faFileInvoice, faFileImport } from '@fortawesome/free-solid-svg-icons';
import FalconCardHeader from 'components/common/FalconCardHeader';
import { Link } from 'react-router';
import { useGetInvoicesQuery } from 'store/api/billingApi';
import {
  formatCurrency,
  formatItalianDate,
  INVOICE_STATUS_LABELS,
} from 'types/billing';
import type { InvoiceSummary, InvoiceStatus } from 'types/billing';

const getStatusBadge = (status: InvoiceStatus) => {
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

const RecentInvoices = () => {
  const { data: issuedData, isLoading: issuedLoading } = useGetInvoicesQuery({
    direction: 'issued',
    pageSize: 5,
  });

  const { data: receivedData, isLoading: receivedLoading } = useGetInvoicesQuery({
    direction: 'received',
    pageSize: 5,
  });

  const isLoading = issuedLoading || receivedLoading;

  if (isLoading) {
    return (
      <Card className="h-100">
        <FalconCardHeader title="Fatture Recenti" titleTag="h6" light />
        <Card.Body className="d-flex align-items-center justify-content-center" style={{ minHeight: 250 }}>
          <Spinner animation="border" />
        </Card.Body>
      </Card>
    );
  }

  // Combine and sort invoices by date
  const allInvoices: (InvoiceSummary & { direction: 'issued' | 'received' })[] = [
    ...(issuedData?.invoices || []).map((inv) => ({ ...inv, direction: 'issued' as const })),
    ...(receivedData?.invoices || []).map((inv) => ({ ...inv, direction: 'received' as const })),
  ]
    .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
    .slice(0, 8);

  return (
    <Card className="h-100">
      <FalconCardHeader
        title="Fatture Recenti"
        titleTag="h6"
        light
        endEl={
          <div className="d-flex gap-2">
            <Link to="/billing/invoices/issued" className="fs-10 text-decoration-none">
              Emesse
            </Link>
            <span className="text-body-tertiary">|</span>
            <Link to="/billing/invoices/received" className="fs-10 text-decoration-none">
              Ricevute
            </Link>
          </div>
        }
      />
      <Card.Body className="p-0">
        {allInvoices.length === 0 ? (
          <div className="text-center py-5 text-body-tertiary">
            <FontAwesomeIcon icon={faFileInvoice} className="fs-3 mb-2 d-block" />
            <span className="fs-10">Nessuna fattura presente</span>
          </div>
        ) : (
          <Table responsive className="fs-10 mb-0">
            <thead className="bg-body-tertiary">
              <tr>
                <th className="border-0 ps-x1">Tipo</th>
                <th className="border-0">Numero</th>
                <th className="border-0">Cliente/Fornitore</th>
                <th className="border-0">Data</th>
                <th className="border-0 text-end">Importo</th>
                <th className="border-0 text-center">Stato</th>
              </tr>
            </thead>
            <tbody>
              {allInvoices.map((invoice) => (
                <tr key={invoice.id} className="align-middle">
                  <td className="ps-x1">
                    <FontAwesomeIcon
                      icon={invoice.direction === 'issued' ? faFileInvoice : faFileImport}
                      className={invoice.direction === 'issued' ? 'text-primary' : 'text-success'}
                      title={invoice.direction === 'issued' ? 'Emessa' : 'Ricevuta'}
                    />
                  </td>
                  <td>
                    <Link
                      to={`/billing/invoices/${invoice.direction}/${invoice.id}`}
                      className="fw-medium"
                    >
                      {invoice.number}
                    </Link>
                  </td>
                  <td className="text-truncate" style={{ maxWidth: 150 }}>
                    {invoice.partyName}
                  </td>
                  <td className="text-body-tertiary">
                    {formatItalianDate(invoice.date)}
                  </td>
                  <td className="text-end fw-medium">
                    {formatCurrency(invoice.totalAmount)}
                  </td>
                  <td className="text-center">
                    <Badge
                      bg={getStatusBadge(invoice.status)}
                      className="text-uppercase fs-11"
                    >
                      {INVOICE_STATUS_LABELS[invoice.status]}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card.Body>
    </Card>
  );
};

export default RecentInvoices;
