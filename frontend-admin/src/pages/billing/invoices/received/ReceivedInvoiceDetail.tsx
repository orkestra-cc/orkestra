import { useParams, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation();
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
    if (!window.confirm(t('billing.receivedDetail.confirm.accept'))) return;

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
    const reason = window.prompt(
      t('billing.receivedDetail.confirm.rejectReason')
    );
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
    const digitsFromNumber = String(invoice.number).replace(/\D/g, '');
    const rawProgressive =
      invoice.progressivoInvio ||
      invoiceId?.slice(-5) ||
      digitsFromNumber.slice(-5).padStart(5, '0') ||
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
    const digitsFromNumber = String(invoice?.number).replace(/\D/g, '');
    const progressive =
      invoice?.progressivoInvio ||
      invoiceId?.slice(-5) ||
      digitsFromNumber.slice(-5).padStart(5, '0') ||
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
          <span className="visually-hidden">
            {t('billing.receivedDetail.loading')}
          </span>
        </Spinner>
      </div>
    );
  }

  // Error state
  if (loadError) {
    return (
      <Alert variant="danger">{t('billing.receivedDetail.loadError')}</Alert>
    );
  }

  // Not found state
  if (!invoice) {
    return (
      <Alert variant="warning">{t('billing.receivedDetail.notFound')}</Alert>
    );
  }

  // Get supplier display name
  const supplierName = invoice.cedentePrestatore
    ? getPartyDisplayName(invoice.cedentePrestatore)
    : t('billing.receivedDetail.summary.notAvailable');

  const headerSubtitle = invoice.sdiStatus
    ? t('billing.receivedDetail.header.subtitleWithSdi', {
        type: invoice.documentType
          ? DOCUMENT_TYPE_LABELS[invoice.documentType]
          : '',
        status: invoice.status ? INVOICE_STATUS_LABELS[invoice.status] : '',
        sdiStatus: SDI_STATUS_LABELS[invoice.sdiStatus]
      })
    : t('billing.receivedDetail.header.subtitle', {
        type: invoice.documentType
          ? DOCUMENT_TYPE_LABELS[invoice.documentType]
          : '',
        status: invoice.status ? INVOICE_STATUS_LABELS[invoice.status] : ''
      });

  return (
    <>
      <PageHeader
        title={t('billing.receivedDetail.header.title', {
          number: invoice.number || ''
        })}
        description={headerSubtitle}
        className="mb-3"
      >
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={() => navigate('/billing/invoices/received')}
        >
          <FontAwesomeIcon icon={faArrowLeft} className="me-1" />
          {t('billing.receivedDetail.header.backToList')}
        </Button>
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={handleViewHtml}
          title={t('billing.receivedDetail.header.preview')}
        >
          <FontAwesomeIcon icon={faEye} className="me-1" />
          {t('billing.receivedDetail.header.preview')}
        </Button>
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={handleViewXml}
          title={t('billing.receivedDetail.header.viewXml')}
        >
          <FontAwesomeIcon icon={faFileCode} className="me-1" />
          XML
        </Button>
        <Button
          variant="orkestra-default"
          size="sm"
          className="me-2"
          onClick={handleDownloadXml}
          title={t('billing.receivedDetail.header.downloadXml')}
        >
          <FontAwesomeIcon icon={faDownload} className="me-1" />
          XML
        </Button>
        <Button
          variant="orkestra-primary"
          size="sm"
          onClick={handleDownloadPdf}
          title={t('billing.receivedDetail.header.downloadPdf')}
        >
          <FontAwesomeIcon icon={faFilePdf} className="me-1" />
          PDF
        </Button>
      </PageHeader>

      {/* Invoice Info Summary */}
      <Card className="mb-3">
        <OrkestraCardHeader
          title={t('billing.receivedDetail.summary.title')}
          light={false}
        />
        <Card.Body>
          <Row>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.number')}
                </small>
                <div className="fw-bold">
                  {invoice.number || t('billing.receivedDetail.summary.dash')}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.date')}
                </small>
                <div className="fw-bold">
                  {invoice.date
                    ? formatItalianDate(invoice.date)
                    : t('billing.receivedDetail.summary.dash')}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.documentType')}
                </small>
                <div className="fw-bold">
                  {invoice.documentType
                    ? DOCUMENT_TYPE_LABELS[invoice.documentType]
                    : t('billing.receivedDetail.summary.dash')}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.status')}
                </small>
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
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.supplier')}
                </small>
                <div className="fw-bold">{supplierName}</div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.taxable')}
                </small>
                <div className="fw-bold">
                  {formatCurrency(invoice.totalTaxableAmount || 0)}
                </div>
              </div>
            </Col>
            <Col md={3}>
              <div className="mb-3">
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.total')}
                </small>
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
                  <small className="text-muted">
                    {t('billing.receivedDetail.summary.sdiId')}
                  </small>
                  <div className="fw-bold">{invoice.sdiIdentifier}</div>
                </div>
              </Col>
            </Row>
          )}
          {invoice.causale && invoice.causale.length > 0 && (
            <Row>
              <Col>
                <small className="text-muted">
                  {t('billing.receivedDetail.summary.causale')}
                </small>
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
        <OrkestraCardHeader
          title={t('billing.receivedDetail.linesCard.title')}
          light={false}
        />
        <Card.Body>
          <div className="table-responsive">
            <Table bordered hover size="sm">
              <thead className="bg-body-tertiary">
                <tr>
                  <th>{t('billing.receivedDetail.linesCard.colNumber')}</th>
                  <th>
                    {t('billing.receivedDetail.linesCard.colDescription')}
                  </th>
                  <th className="text-end">
                    {t('billing.receivedDetail.linesCard.colQuantity')}
                  </th>
                  <th>{t('billing.receivedDetail.linesCard.colUnit')}</th>
                  <th className="text-end">
                    {t('billing.receivedDetail.linesCard.colUnitPrice')}
                  </th>
                  <th className="text-end">
                    {t('billing.receivedDetail.linesCard.colVat')}
                  </th>
                  <th>{t('billing.receivedDetail.linesCard.colNature')}</th>
                  <th className="text-end">
                    {t('billing.receivedDetail.linesCard.colTotal')}
                  </th>
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
                        : t('billing.receivedDetail.summary.dash')}
                    </td>
                    <td className="text-end">
                      {formatCurrency(line.unitPrice)}
                    </td>
                    <td className="text-end">{line.vatRate}%</td>
                    <td>
                      {line.vatNature ||
                        t('billing.receivedDetail.summary.dash')}
                    </td>
                    <td className="text-end fw-bold">
                      {formatCurrency(line.totalPrice)}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot className="bg-body-tertiary">
                <tr>
                  <td colSpan={7} className="text-end">
                    {t('billing.receivedDetail.linesCard.footerTaxable')}
                  </td>
                  <td className="text-end">
                    {formatCurrency(invoice.totalTaxableAmount || 0)}
                  </td>
                </tr>
                <tr>
                  <td colSpan={7} className="text-end">
                    {t('billing.receivedDetail.linesCard.footerVat')}
                  </td>
                  <td className="text-end">
                    {formatCurrency(invoice.totalVatAmount || 0)}
                  </td>
                </tr>
                <tr className="fw-bold">
                  <td colSpan={7} className="text-end">
                    {t('billing.receivedDetail.linesCard.footerTotal')}
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
          <OrkestraCardHeader
            title={t('billing.receivedDetail.paymentCard.title')}
            light={false}
          />
          <Card.Body>
            <Row>
              <Col md={3}>
                <small className="text-muted">
                  {t('billing.receivedDetail.paymentCard.condition')}
                </small>
                <div>
                  {invoice.paymentTerms.condition
                    ? PAYMENT_CONDITION_LABELS[invoice.paymentTerms.condition]
                    : t('billing.receivedDetail.summary.dash')}
                </div>
              </Col>
              <Col md={3}>
                <small className="text-muted">
                  {t('billing.receivedDetail.paymentCard.method')}
                </small>
                <div>
                  {invoice.paymentTerms.paymentMethod
                    ? PAYMENT_METHOD_LABELS[invoice.paymentTerms.paymentMethod]
                    : t('billing.receivedDetail.summary.dash')}
                </div>
              </Col>
              {invoice.paymentTerms.dueDate && (
                <Col md={3}>
                  <small className="text-muted">
                    {t('billing.receivedDetail.paymentCard.dueDate')}
                  </small>
                  <div>{formatItalianDate(invoice.paymentTerms.dueDate)}</div>
                </Col>
              )}
              {invoice.paymentTerms.beneficiario && (
                <Col md={3}>
                  <small className="text-muted">
                    {t('billing.receivedDetail.paymentCard.beneficiario')}
                  </small>
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
                    <small className="text-muted">
                      {t('billing.receivedDetail.paymentCard.istituto')}
                    </small>
                    <div>{invoice.paymentTerms.istitutoFinanziario}</div>
                  </Col>
                )}
                {invoice.paymentTerms.iban && (
                  <Col md={4}>
                    <small className="text-muted">
                      {t('billing.receivedDetail.paymentCard.iban')}
                    </small>
                    <div>{invoice.paymentTerms.iban}</div>
                  </Col>
                )}
                {invoice.paymentTerms.bic && (
                  <Col md={2}>
                    <small className="text-muted">
                      {t('billing.receivedDetail.paymentCard.bic')}
                    </small>
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
              {isRejecting
                ? t('billing.receivedDetail.actions.processing')
                : t('billing.receivedDetail.actions.reject')}
            </Button>
            <Button
              variant="success"
              onClick={handleAccept}
              disabled={isProcessing}
            >
              <FontAwesomeIcon icon={faCheck} className="me-1" />
              {isAccepting
                ? t('billing.receivedDetail.actions.processing')
                : t('billing.receivedDetail.actions.accept')}
            </Button>
          </Card.Body>
        </Card>
      )}
    </>
  );
};

export default ReceivedInvoiceDetail;
