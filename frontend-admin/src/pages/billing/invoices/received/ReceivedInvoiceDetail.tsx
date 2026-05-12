import { useParams, useNavigate } from 'react-router';
import {
  Card,
  Button,
  Alert,
  Row,
  Col,
  Table,
  Spinner,
  Badge
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faArrowLeft,
  faEye,
  faFileCode,
  faDownload,
  faFilePdf,
  faCheck,
  faTimes
} from '@fortawesome/free-solid-svg-icons';
import {
  useGetInvoiceQuery,
  useLazyGetInvoiceXmlQuery,
  useLazyGetInvoiceHtmlQuery,
  useLazyGetInvoicePdfQuery,
  useAcceptInvoiceMutation,
  useRejectInvoiceMutation
} from 'store/api/billingApi';
import type { InvoiceStatus } from 'types/billing';
import {
  DOCUMENT_TYPE_LABELS,
  INVOICE_STATUS_LABELS,
  SDI_STATUS_LABELS,
  PAYMENT_METHOD_LABELS,
  PAYMENT_CONDITION_LABELS,
  UNIT_OF_MEASURE_LABELS,
  formatCurrency,
  formatItalianDate,
  getPartyDisplayName
} from 'types/billing';
import PageHeader from 'components/common/PageHeader';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

const getStatusBadgeVariant = (status: InvoiceStatus): string => {
  const variants: Record<InvoiceStatus, string> = {
    draft: 'secondary',
    pending: 'warning',
    sent: 'info',
    delivered: 'primary',
    rejected: 'danger',
    accepted: 'success',
    paid: 'success',
    cancelled: 'secondary'
  };
  return variants[status] || 'secondary';
};

const ReceivedInvoiceDetail: React.FC = () => {
  const { invoiceId } = useParams<{ invoiceId: string }>();
  const navigate = useNavigate();

  // API hooks
  const {
    data: invoice,
    isLoading: isLoadingInvoice,
    error: loadError,
    refetch
  } = useGetInvoiceQuery(invoiceId!, { skip: !invoiceId });
  const [getInvoiceXml] = useLazyGetInvoiceXmlQuery();
  const [getInvoiceHtml] = useLazyGetInvoiceHtmlQuery();
  const [getInvoicePdf] = useLazyGetInvoicePdfQuery();
  const [acceptInvoice, { isLoading: isAccepting }] =
    useAcceptInvoiceMutation();
  const [rejectInvoice, { isLoading: isRejecting }] =
    useRejectInvoiceMutation();

  // Check if invoice can be responded to
  const canRespond = invoice?.status === 'pending';
  const isProcessing = isAccepting || isRejecting;

  // Handle accept invoice
  const handleAccept = async () => {
    if (!invoiceId) return;
    if (!window.confirm('Sei sicuro di voler accettare questa fattura?'))
      return;

    try {
      await acceptInvoice(invoiceId).unwrap();
      refetch();
    } catch (err) {
      console.error('Failed to accept invoice:', err);
    }
  };

  // Handle reject invoice
  const handleReject = async () => {
    if (!invoiceId) return;
    const reason = window.prompt('Inserisci il motivo del rifiuto:');
    if (!reason) return;

    try {
      await rejectInvoice({ id: invoiceId, reason }).unwrap();
      refetch();
    } catch (err) {
      console.error('Failed to reject invoice:', err);
    }
  };

  // View XML
  const handleViewXml = async () => {
    try {
      const result = await getInvoiceXml(invoiceId!).unwrap();
      const encoder = new TextEncoder();
      const utf8Bytes = encoder.encode(result);
      const blob = new Blob([utf8Bytes], {
        type: 'application/xml; charset=utf-8'
      });
      const url = window.URL.createObjectURL(blob);
      window.open(url, '_blank');
    } catch {
      console.error('Error loading XML');
    }
  };

  // Download XML file
  // FatturaPA filename format: {CountryCode}{IdCodice}_{ProgressivoInvio}.xml
  // IdCodice = P.IVA (fiscalIdCode), per FatturaPA specifications
  const generateFatturaFilename = () => {
    const cedente = invoice?.cedentePrestatore;
    if (!cedente) {
      return `fattura_${invoiceId}.xml`;
    }
    // Always use fiscalIdCode (P.IVA) for filename - FatturaPA spec requirement
    const fiscalCode = cedente.fiscalIdCode;
    if (!fiscalCode) {
      return `fattura_${invoiceId}.xml`;
    }
    const countryCode = cedente.fiscalIdCountry || 'IT';
    // Progressive: use last 5 chars of progressivoInvio (filename limit is 5, XML allows 10)
    const rawProgressive =
      invoice.progressivoInvio ||
      invoiceId?.slice(-5) ||
      String(invoice.number).replace(/\D/g, '').slice(-5).padStart(5, '0') ||
      '00001';
    // FatturaPA filename spec: progressive max 5 alphanumeric chars
    const progressive = rawProgressive.slice(-5);
    return `${countryCode}${fiscalCode}_${progressive}.xml`;
  };

  const handleDownloadXml = async () => {
    try {
      const result = await getInvoiceXml(invoiceId!).unwrap();
      const encoder = new TextEncoder();
      const utf8Bytes = encoder.encode(result);
      const blob = new Blob([utf8Bytes], {
        type: 'application/xml; charset=utf-8'
      });
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = generateFatturaFilename();
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
    } catch {
      console.error('Error downloading XML');
    }
  };

  // View HTML preview
  const handleViewHtml = async () => {
    try {
      const result = await getInvoiceHtml(invoiceId!).unwrap();
      const blob = new Blob([result], { type: 'text/html' });
      const url = window.URL.createObjectURL(blob);
      window.open(url, '_blank');
    } catch {
      console.error('Error loading HTML preview');
    }
  };

  // Generate PDF filename
  const generatePdfFilename = () => {
    const cedente = invoice?.cedentePrestatore;
    const invoiceNumber = invoice?.number || invoiceId;
    if (!cedente) {
      return `fattura_${invoiceNumber}.pdf`;
    }
    const fiscalCode = cedente.codiceFiscale || cedente.fiscalIdCode;
    if (!fiscalCode) {
      return `fattura_${invoiceNumber}.pdf`;
    }
    const countryCode = cedente.fiscalIdCountry || 'IT';
    const progressive =
      invoice?.progressivoInvio ||
      invoiceId?.slice(-5) ||
      String(invoice?.number).replace(/\D/g, '').slice(-5).padStart(5, '0') ||
      '00001';
    return `${countryCode}${fiscalCode}_${progressive}.pdf`;
  };

  // Download PDF
  const handleDownloadPdf = async () => {
    try {
      await getInvoicePdf({
        id: invoiceId!,
        filename: generatePdfFilename()
      }).unwrap();
    } catch {
      console.error('Error downloading PDF');
    }
  };

  // Loading state
  if (isLoadingInvoice) {
    return (
      <div
        className="d-flex justify-content-center align-items-center"
        style={{ minHeight: '400px' }}
      >
        <Spinner animation="border" role="status">
          <span className="visually-hidden">Caricamento...</span>
        </Spinner>
      </div>
    );
  }

  // Error state
  if (loadError) {
    return (
      <Alert variant="danger">
        Errore durante il caricamento della fattura. Riprova più tardi.
      </Alert>
    );
  }

  // Not found state
  if (!invoice) {
    return <Alert variant="warning">Fattura non trovata.</Alert>;
  }

  // Get supplier display name
  const supplierName = invoice.cedentePrestatore
    ? getPartyDisplayName(invoice.cedentePrestatore)
    : 'N/A';

  return (
    <>
      <PageHeader
        title={`Fattura ${invoice.number || ''}`}
        description={`${invoice.documentType ? DOCUMENT_TYPE_LABELS[invoice.documentType] : ''} - ${invoice.status ? INVOICE_STATUS_LABELS[invoice.status] : ''}${invoice.sdiStatus ? ` (${SDI_STATUS_LABELS[invoice.sdiStatus]})` : ''}`}
        className="mb-3"
      >
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={() => navigate('/billing/invoices/received')}
        >
          <FontAwesomeIcon icon={faArrowLeft} className="me-1" />
          Torna alla lista
        </Button>
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={handleViewHtml}
          title="Anteprima"
        >
          <FontAwesomeIcon icon={faEye} className="me-1" />
          Anteprima
        </Button>
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={handleViewXml}
          title="Visualizza XML"
        >
          <FontAwesomeIcon icon={faFileCode} className="me-1" />
          XML
        </Button>
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={handleDownloadXml}
          title="Scarica XML"
        >
          <FontAwesomeIcon icon={faDownload} className="me-1" />
          XML
        </Button>
        <Button
          variant="orkestra-primary"
          size="sm"
          onClick={handleDownloadPdf}
          title="Scarica PDF"
        >
          <FontAwesomeIcon icon={faFilePdf} className="me-1" />
          PDF
        </Button>
      </PageHeader>

      {/* Invoice Info Summary */}
      <Card className="mb-3">
        <OrkestraCardHeader title="Riepilogo Fattura" light={false} />
        <Card.Body>
          <Row>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">Numero</small>
                <div className="fw-bold">{invoice.number || '-'}</div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">Data</small>
                <div className="fw-bold">
                  {invoice.date ? formatItalianDate(invoice.date) : '-'}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">Tipo Documento</small>
                <div className="fw-bold">
                  {invoice.documentType
                    ? DOCUMENT_TYPE_LABELS[invoice.documentType]
                    : '-'}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">Stato</small>
                <div>
                  {invoice.status && (
                    <Badge
                      bg={getStatusBadgeVariant(invoice.status)}
                      className="text-uppercase"
                    >
                      {INVOICE_STATUS_LABELS[invoice.status]}
                    </Badge>
                  )}
                </div>
              </div>
            </Col>
          </Row>
          <Row>
            <Col md={6}>
              <div className="mb-3">
                <small className="text-muted">Fornitore</small>
                <div className="fw-bold">{supplierName}</div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">Imponibile</small>
                <div className="fw-bold">
                  {formatCurrency(invoice.totalTaxableAmount || 0)}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">Totale</small>
                <div className="fw-bold fs-5 text-primary">
                  {formatCurrency(invoice.totalAmount || 0)}
                </div>
              </div>
            </Col>
          </Row>
          {invoice.sdiIdentifier && (
            <Row>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">ID SDI</small>
                  <div className="fw-bold">{invoice.sdiIdentifier}</div>
                </div>
              </Col>
            </Row>
          )}
          {invoice.causale && invoice.causale.length > 0 && (
            <Row>
              <Col>
                <small className="text-muted">Causale</small>
                {invoice.causale.map((c, idx) => (
                  <div key={idx}>{c}</div>
                ))}
              </Col>
            </Row>
          )}
        </Card.Body>
      </Card>

      {/* Invoice Lines */}
      <Card className="mb-3">
        <OrkestraCardHeader title="Dettaglio Righe" light={false} />
        <Card.Body>
          <div className="table-responsive">
            <Table bordered hover size="sm">
              <thead className="bg-body-tertiary">
                <tr>
                  <th>#</th>
                  <th>Descrizione</th>
                  <th className="text-end">Qtà</th>
                  <th>U.M.</th>
                  <th className="text-end">Prezzo Unit.</th>
                  <th className="text-end">IVA %</th>
                  <th>Natura</th>
                  <th className="text-end">Totale</th>
                </tr>
              </thead>
              <tbody>
                {(invoice.lines || []).map(line => (
                  <tr key={line.lineNumber}>
                    <td>{line.lineNumber}</td>
                    <td>{line.description}</td>
                    <td className="text-end">{line.quantity}</td>
                    <td>
                      {line.unitOfMeasure
                        ? UNIT_OF_MEASURE_LABELS[line.unitOfMeasure]
                        : '-'}
                    </td>
                    <td className="text-end">
                      {formatCurrency(line.unitPrice)}
                    </td>
                    <td className="text-end">{line.vatRate}%</td>
                    <td>{line.vatNature || '-'}</td>
                    <td className="text-end fw-bold">
                      {formatCurrency(line.totalPrice)}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot className="bg-body-tertiary">
                <tr>
                  <td colSpan={7} className="text-end">
                    Imponibile:
                  </td>
                  <td className="text-end">
                    {formatCurrency(invoice.totalTaxableAmount || 0)}
                  </td>
                </tr>
                <tr>
                  <td colSpan={7} className="text-end">
                    IVA:
                  </td>
                  <td className="text-end">
                    {formatCurrency(invoice.totalVatAmount || 0)}
                  </td>
                </tr>
                <tr className="fw-bold">
                  <td colSpan={7} className="text-end">
                    Totale:
                  </td>
                  <td className="text-end">
                    {formatCurrency(invoice.totalAmount || 0)}
                  </td>
                </tr>
              </tfoot>
            </Table>
          </div>
        </Card.Body>
      </Card>

      {/* Payment Terms */}
      {invoice.paymentTerms && (
        <Card className="mb-3">
          <OrkestraCardHeader title="Termini di Pagamento" light={false} />
          <Card.Body>
            <Row>
              <Col md={3}>
                <small className="text-muted">Condizione</small>
                <div>
                  {invoice.paymentTerms.condition
                    ? PAYMENT_CONDITION_LABELS[invoice.paymentTerms.condition]
                    : '-'}
                </div>
              </Col>
              <Col md={3}>
                <small className="text-muted">Metodo</small>
                <div>
                  {invoice.paymentTerms.paymentMethod
                    ? PAYMENT_METHOD_LABELS[invoice.paymentTerms.paymentMethod]
                    : '-'}
                </div>
              </Col>
              {invoice.paymentTerms.dueDate && (
                <Col md={3}>
                  <small className="text-muted">Scadenza</small>
                  <div>{formatItalianDate(invoice.paymentTerms.dueDate)}</div>
                </Col>
              )}
              {invoice.paymentTerms.beneficiario && (
                <Col md={3}>
                  <small className="text-muted">Beneficiario</small>
                  <div>{invoice.paymentTerms.beneficiario}</div>
                </Col>
              )}
            </Row>
            {(invoice.paymentTerms.iban ||
              invoice.paymentTerms.bic ||
              invoice.paymentTerms.istitutoFinanziario) && (
              <Row className="mt-3">
                {invoice.paymentTerms.istitutoFinanziario && (
                  <Col md={3}>
                    <small className="text-muted">Istituto Finanziario</small>
                    <div>{invoice.paymentTerms.istitutoFinanziario}</div>
                  </Col>
                )}
                {invoice.paymentTerms.iban && (
                  <Col md={4}>
                    <small className="text-muted">IBAN</small>
                    <div>{invoice.paymentTerms.iban}</div>
                  </Col>
                )}
                {invoice.paymentTerms.bic && (
                  <Col md={2}>
                    <small className="text-muted">BIC/SWIFT</small>
                    <div>{invoice.paymentTerms.bic}</div>
                  </Col>
                )}
              </Row>
            )}
          </Card.Body>
        </Card>
      )}

      {/* Action Buttons */}
      {canRespond && (
        <Card>
          <Card.Body className="d-flex justify-content-end gap-2">
            <Button
              variant="danger"
              onClick={handleReject}
              disabled={isProcessing}
            >
              <FontAwesomeIcon icon={faTimes} className="me-1" />
              {isRejecting ? 'Elaborazione...' : 'Rifiuta'}
            </Button>
            <Button
              variant="success"
              onClick={handleAccept}
              disabled={isProcessing}
            >
              <FontAwesomeIcon icon={faCheck} className="me-1" />
              {isAccepting ? 'Elaborazione...' : 'Accetta'}
            </Button>
          </Card.Body>
        </Card>
      )}
    </>
  );
};

export default ReceivedInvoiceDetail;
