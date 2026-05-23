import { useParams, useNavigate } from 'react-router-dom';
import { Row, Col, Card, Badge, Button, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faArrowLeft, faDownload } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import { useGetSalesReportQuery } from '../../../store/api/salesApi';

import runtimeConfig from 'config/environment';

const GRADE_COLORS: Record<string, string> = {
  A: 'success',
  B: 'primary',
  C: 'warning',
  D: 'danger',
  F: 'dark'
};

const SalesReportDetail = () => {
  const { t } = useTranslation();
  const { uuid } = useParams<{ uuid: string }>();
  const navigate = useNavigate();
  const { data: report, isLoading } = useGetSalesReportQuery(uuid || '');
  const backendUrl = runtimeConfig.apiUrl;

  if (isLoading || !report) {
    return (
      <Row className="g-3 mb-3">
        <Col>
          <Card>
            <Card.Body className="text-center py-5">
              <Spinner />
            </Card.Body>
          </Card>
        </Col>
      </Row>
    );
  }

  return (
    <Row className="g-3 mb-3">
      <Col xxl={12}>
        <Card>
          <Card.Header className="d-flex justify-content-between align-items-center">
            <div className="d-flex align-items-center gap-2">
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={() => navigate('/sales/reports')}
              >
                <FontAwesomeIcon icon={faArrowLeft} />
              </Button>
              <div>
                <h5 className="mb-0">{report.companyName}</h5>
                <small className="text-muted">{report.companyUrl}</small>
              </div>
            </div>
            <div className="d-flex align-items-center gap-2">
              <Badge
                bg={GRADE_COLORS[report.grade] || 'secondary'}
                className="fs-6"
              >
                {t('sales.reports.detail.scoreBadge', {
                  score: report.score,
                  grade: report.grade
                })}
              </Badge>
              <a
                href={`${backendUrl}/v1/sales/reports/${report.uuid}/md`}
                className="btn btn-outline-primary btn-sm"
                download
              >
                <FontAwesomeIcon icon={faDownload} className="me-1" />
                {t('sales.reports.detail.downloadMd')}
              </a>
            </div>
          </Card.Header>
          <Card.Body>
            <div
              style={{ maxHeight: '75vh', overflow: 'auto' }}
              dangerouslySetInnerHTML={{
                __html: markdownToHtml(report.contentMd || '')
              }}
            />
          </Card.Body>
          <Card.Footer className="text-muted">
            <small>
              {t('sales.reports.detail.footer', {
                date: new Date(report.createdAt).toLocaleString(),
                job: report.jobUuid
              })}
            </small>
          </Card.Footer>
        </Card>
      </Col>
    </Row>
  );
};

/** Simple Markdown to HTML */
function markdownToHtml(md: string): string {
  let html = md
    .replace(
      /```(\w*)\n([\s\S]*?)```/g,
      '<pre class="bg-body-tertiary p-3 rounded"><code>$2</code></pre>'
    )
    .replace(/^#### (.+)$/gm, '<h6 class="mt-3">$1</h6>')
    .replace(/^### (.+)$/gm, '<h5 class="mt-3">$1</h5>')
    .replace(/^## (.+)$/gm, '<h4 class="mt-4 mb-2 pb-1 border-bottom">$1</h4>')
    .replace(/^# (.+)$/gm, '<h3 class="mb-3">$1</h3>')
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/^---$/gm, '<hr class="my-3" />')
    .replace(/^\|(.+)\|$/gm, match => {
      const cells = match.split('|').filter(c => c.trim() !== '');
      if (cells.every(c => /^[\s-:]+$/.test(c))) return '<!--table-sep-->';
      const row = cells
        .map(c => `<td class="px-2 py-1">${c.trim()}</td>`)
        .join('');
      return `<tr>${row}</tr>`;
    })
    .replace(
      /^> (.+)$/gm,
      '<div class="alert alert-warning py-1 px-2">$1</div>'
    )
    .replace(/^- (.+)$/gm, '<li>$1</li>')
    .replace(/\n\n/g, '</p><p>')
    .replace(/ {2}\n/g, '<br />');

  html = html.replace(
    /((?:<tr>.*<\/tr>\n?)+)/g,
    '<table class="table table-sm table-bordered mb-3">$1</table>'
  );
  html = html.replace(/<!--table-sep-->\n?/g, '');
  html = html.replace(/((?:<li>.*<\/li>\n?)+)/g, '<ul class="mb-2">$1</ul>');

  return `<div class="sales-report">${html}</div>`;
}

export default SalesReportDetail;
