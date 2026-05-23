import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  Row,
  Col,
  Card,
  Table,
  Badge,
  Button,
  Spinner,
  Tab,
  Tabs,
  Alert
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faTasks,
  faSync,
  faTrash,
  faCheckCircle,
  faTimesCircle,
  faClock,
  faPlay,
  faRedo,
  faFileAlt,
  faSpinner,
  faArrowLeft
} from '@fortawesome/free-solid-svg-icons';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import Background from 'components/common/Background';
import greetingsBg from 'assets/img/illustrations/ticket-greetings-bg.png';
import {
  useListSalesJobsQuery,
  useGetSalesJobQuery,
  useCancelSalesJobMutation,
  useRetrySalesJobMutation,
  useRerunSalesJobAgentsMutation
} from '../../../store/api/salesApi';

const STATUS_COLORS: Record<string, string> = {
  queued: 'secondary',
  discovery: 'info',
  analysis: 'primary',
  synthesis: 'warning',
  completed: 'success',
  failed: 'danger',
  batch_pending: 'info',
  cancelled: 'dark'
};

const STATUS_ICONS: Record<string, any> = {
  queued: faClock,
  discovery: faSpinner,
  analysis: faSpinner,
  batch_pending: faClock,
  synthesis: faSpinner,
  completed: faCheckCircle,
  failed: faTimesCircle,
  cancelled: faTimesCircle
};

const PHASE_ICONS: Record<string, any> = {
  pending: faClock,
  running: faPlay,
  completed: faCheckCircle,
  failed: faTimesCircle,
  skipped: faTimesCircle
};

const PHASE_COLORS: Record<string, string> = {
  pending: 'text-muted',
  running: 'text-primary',
  completed: 'text-success',
  failed: 'text-danger',
  skipped: 'text-muted'
};

const RUNNING_STATUSES = [
  'queued',
  'discovery',
  'analysis',
  'batch_pending',
  'synthesis'
];

function isRunning(status: string) {
  return RUNNING_STATUSES.includes(status);
}

// ─── Agent Result Card ───

function AgentResultCard({ result }: { result: any }) {
  const { t } = useTranslation();
  let findings: any = null;
  try {
    findings =
      typeof result.findings === 'string'
        ? JSON.parse(result.findings)
        : result.findings;
  } catch {
    /* ignore */
  }

  return (
    <Card className="mb-2">
      <Card.Header className="d-flex justify-content-between align-items-center py-2 px-3">
        <div>
          <strong>{formatAgentName(result.agentName)}</strong>
          {result.error && (
            <Badge bg="danger" className="ms-2">
              {t('sales.jobs.detail.agentErrorBadge')}
            </Badge>
          )}
        </div>
        <div className="d-flex gap-2 align-items-center">
          <Badge
            bg={
              result.score >= 75
                ? 'success'
                : result.score >= 50
                  ? 'warning'
                  : result.score > 0
                    ? 'danger'
                    : 'secondary'
            }
          >
            {t('sales.jobs.detail.agentScoreBadge', { score: result.score })}
          </Badge>
          {result.latencyMs > 0 && (
            <small className="text-muted">
              {t('sales.jobs.detail.agentLatencySeconds', {
                seconds: (result.latencyMs / 1000).toFixed(1)
              })}
            </small>
          )}
        </div>
      </Card.Header>
      <Card.Body className="p-3">
        {result.error && <div className="text-danger mb-2">{result.error}</div>}
        {findings && (
          <pre
            className="bg-body-tertiary p-3 rounded mb-0"
            style={{ maxHeight: 400, overflow: 'auto', fontSize: '0.8rem' }}
          >
            {JSON.stringify(findings, null, 2)}
          </pre>
        )}
      </Card.Body>
    </Card>
  );
}

function formatAgentName(name: string) {
  return name
    .split('-')
    .map((w: string) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
}

// ─── Job Detail Page (URL: /sales/jobs/:uuid) ───

export function JobDetailPage() {
  const { t } = useTranslation();
  const { uuid } = useParams<{ uuid: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { data: job, isLoading } = useGetSalesJobQuery(uuid || '', {
    pollingInterval: uuid ? 3000 : 0,
    skip: !uuid
  });
  const [retryJob, { isLoading: retrying }] = useRetrySalesJobMutation();
  const [rerunAgents, { isLoading: rerunning }] =
    useRerunSalesJobAgentsMutation();
  const [cancelJob] = useCancelSalesJobMutation();

  if (isLoading || !job) {
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

  const agentResults = job.agentResults || [];
  const running = isRunning(job.status);
  const failedAgents = agentResults.filter(
    (r: any) =>
      r &&
      (r.error || (r.score === 0 && (!r.findings || r.findings.length <= 2)))
  );
  const hasFailedAgents = failedAgents.length > 0 && !running;
  const elapsed = job.completedAt
    ? (
        (new Date(job.completedAt).getTime() -
          new Date(job.createdAt).getTime()) /
        1000
      ).toFixed(0)
    : ((Date.now() - new Date(job.createdAt).getTime()) / 1000).toFixed(0);

  return (
    <>
      {/* Header */}
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <div className="d-flex align-items-center gap-3">
                <Button
                  variant="outline-secondary"
                  size="sm"
                  onClick={() => navigate('/sales/jobs')}
                >
                  <FontAwesomeIcon icon={faArrowLeft} />
                </Button>
                <div>
                  <h5 className="mb-0 d-flex align-items-center gap-2">
                    <FontAwesomeIcon
                      icon={STATUS_ICONS[job.status] || faClock}
                      spin={running}
                      className={
                        running
                          ? 'text-primary'
                          : STATUS_COLORS[job.status] === 'success'
                            ? 'text-success'
                            : 'text-danger'
                      }
                    />
                    {job.companyUrl}
                  </h5>
                  <small className="text-muted">
                    {t('sales.jobs.detail.subtitle', {
                      status: job.status.toUpperCase(),
                      elapsed
                    })}
                    {running && t('sales.jobs.detail.subtitleRefreshing')}
                  </small>
                </div>
              </div>
              <div className="d-flex align-items-center gap-2">
                {(job.totalScore ?? 0) > 0 && (
                  <Badge
                    bg={
                      (job.totalScore ?? 0) >= 75
                        ? 'success'
                        : (job.totalScore ?? 0) >= 50
                          ? 'warning'
                          : 'danger'
                    }
                    className="fs-6"
                  >
                    {t('sales.jobs.detail.scoreBadge', {
                      score: job.totalScore,
                      grade: job.grade
                    })}
                  </Badge>
                )}
                <Badge
                  bg={STATUS_COLORS[job.status] || 'secondary'}
                  className="fs-6"
                >
                  {job.status}
                </Badge>
                {running && (
                  <Button
                    variant="outline-danger"
                    size="sm"
                    onClick={() => cancelJob(job.uuid)}
                  >
                    <FontAwesomeIcon icon={faTrash} className="me-1" />{' '}
                    {t('sales.jobs.detail.cancelButton')}
                  </Button>
                )}
                {(job.status === 'failed' || job.status === 'cancelled') && (
                  <Button
                    variant="outline-warning"
                    size="sm"
                    disabled={retrying}
                    onClick={async () => {
                      const result = await retryJob(job.uuid).unwrap();
                      navigate(`/sales/jobs/${result.jobId}`);
                    }}
                  >
                    {retrying ? (
                      <Spinner size="sm" className="me-1" />
                    ) : (
                      <FontAwesomeIcon icon={faRedo} className="me-1" />
                    )}
                    {t('sales.jobs.detail.retryButton')}
                  </Button>
                )}
                {hasFailedAgents && (
                  <Button
                    variant="warning"
                    size="sm"
                    disabled={rerunning}
                    onClick={async () => {
                      await rerunAgents(job.uuid).unwrap();
                    }}
                  >
                    {rerunning ? (
                      <Spinner size="sm" className="me-1" />
                    ) : (
                      <FontAwesomeIcon icon={faRedo} className="me-1" />
                    )}
                    {t('sales.jobs.detail.rerunFailedButton', {
                      count: failedAgents.length
                    })}
                  </Button>
                )}
                {job.reportUuid && (
                  <Link
                    to={`/sales/reports/${job.reportUuid}`}
                    className="btn btn-outline-success btn-sm"
                  >
                    <FontAwesomeIcon icon={faFileAlt} className="me-1" />{' '}
                    {t('sales.jobs.detail.reportLink')}
                  </Link>
                )}
                <Button
                  variant="outline-danger"
                  size="sm"
                  onClick={() => {
                    if (window.confirm(t('sales.jobs.detail.deleteConfirm'))) {
                      cancelJob(job.uuid);
                      navigate('/sales/jobs');
                    }
                  }}
                >
                  <FontAwesomeIcon icon={faTrash} className="me-1" />{' '}
                  {t('sales.jobs.detail.deleteButton')}
                </Button>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      {/* Pipeline Progress */}
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card>
            <Card.Header>
              <h6 className="mb-0">{t('sales.jobs.detail.pipelineTitle')}</h6>
            </Card.Header>
            <Card.Body className="py-2">
              <div className="d-flex gap-4">
                {(job.phases || []).map((phase: any, i: number) => {
                  const icon = PHASE_ICONS[phase.status] || faClock;
                  const color = PHASE_COLORS[phase.status] || 'text-muted';
                  const duration =
                    phase.startedAt && phase.completedAt
                      ? t('sales.jobs.detail.phaseDurationSeconds', {
                          seconds: (
                            (new Date(phase.completedAt).getTime() -
                              new Date(phase.startedAt).getTime()) /
                            1000
                          ).toFixed(1)
                        })
                      : phase.startedAt && phase.status === 'running'
                        ? t('sales.jobs.detail.phaseDurationRunning', {
                            seconds: (
                              (Date.now() -
                                new Date(phase.startedAt).getTime()) /
                              1000
                            ).toFixed(0)
                          })
                        : '';

                  return (
                    <div key={i} className="d-flex align-items-center gap-2">
                      <FontAwesomeIcon
                        icon={icon}
                        spin={phase.status === 'running'}
                        className={color}
                      />
                      <div>
                        <strong className="text-capitalize">
                          {phase.name}
                        </strong>
                        {duration && (
                          <small className="text-muted ms-1">{duration}</small>
                        )}
                      </div>
                      {i < (job.phases || []).length - 1 && (
                        <span className="text-muted mx-1">→</span>
                      )}
                    </div>
                  );
                })}
              </div>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      {/* Error */}
      {job.errorMessage && (
        <Row className="g-3 mb-3">
          <Col xxl={12}>
            <Card border="danger">
              <Card.Body className="text-danger">
                <strong>{t('sales.jobs.detail.errorPrefix')}</strong>{' '}
                {job.errorMessage}
              </Card.Body>
            </Card>
          </Col>
        </Row>
      )}

      {/* Batch pending info */}
      {job.status === 'batch_pending' && (
        <Row className="g-3 mb-3">
          <Col>
            <Alert
              variant="info"
              className="d-flex align-items-center gap-2 mb-0"
            >
              <FontAwesomeIcon icon={faClock} />
              <div>
                <strong>{t('sales.jobs.detail.batchTitle')}</strong>
                {t('sales.jobs.detail.batchBody')}
              </div>
            </Alert>
          </Col>
        </Row>
      )}

      {/* Results */}
      {(agentResults.length > 0 || job.status === 'completed') && (
        <Row className="g-3 mb-3">
          <Col xxl={12}>
            <Card>
              <Card.Body>
                <Tabs
                  activeKey={
                    searchParams.get('tab') ||
                    (agentResults.length > 0 ? 'agents' : 'raw')
                  }
                  onSelect={k => {
                    if (!k) return;
                    setSearchParams(
                      prev => {
                        prev.set('tab', k);
                        return prev;
                      },
                      { replace: true }
                    );
                  }}
                  className="mb-3"
                >
                  {agentResults.length > 0 && (
                    <Tab
                      eventKey="agents"
                      title={t('sales.jobs.detail.agentResultsTab', {
                        count: agentResults.length
                      })}
                    >
                      {agentResults.map((r: any, i: number) => (
                        <AgentResultCard key={i} result={r} />
                      ))}
                    </Tab>
                  )}
                  <Tab eventKey="raw" title={t('sales.jobs.detail.rawTab')}>
                    <pre
                      className="bg-body-tertiary p-3 rounded"
                      style={{
                        maxHeight: 500,
                        overflow: 'auto',
                        fontSize: '0.75rem'
                      }}
                    >
                      {JSON.stringify(job, null, 2)}
                    </pre>
                  </Tab>
                </Tabs>
              </Card.Body>
            </Card>
          </Col>
        </Row>
      )}

      {/* Footer */}
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <small className="text-muted">
            {t('sales.jobs.detail.footerCreated', {
              date: new Date(job.createdAt).toLocaleString()
            })}
            {job.completedAt &&
              t('sales.jobs.detail.footerCompleted', {
                date: new Date(job.completedAt).toLocaleString()
              })}
            {t('sales.jobs.detail.footerUuid', { uuid: job.uuid })}
          </small>
        </Col>
      </Row>
    </>
  );
}

// ─── Jobs List Page (URL: /sales/jobs) ───

const JobsPage = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data, isLoading, refetch } = useListSalesJobsQuery(
    { pageSize: 50 },
    { pollingInterval: 10000 }
  );
  const [cancelJob] = useCancelSalesJobMutation();
  const [retryJob] = useRetrySalesJobMutation();

  const jobs = data?.jobs || [];
  const runningCount = jobs.filter((j: any) => isRunning(j.status)).length;

  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card className="bg-body-tertiary dark__bg-opacity-50 shadow-none h-100">
            <Background
              image={greetingsBg}
              className="bg-card d-none d-sm-block"
            />
            <Card.Header className="d-flex align-items-center z-1 p-0">
              <div className="bg-primary rounded-circle p-3 ms-3">
                <FontAwesomeIcon
                  icon={faTasks}
                  className="text-white"
                  size="2x"
                />
              </div>
              <div className="ms-3">
                <h6 className="mb-1 text-primary">{t('sales.kicker')}</h6>
                <h4 className="mb-0 text-primary fw-bold">
                  {t('sales.jobs.title')}
                  <span className="text-info fw-medium">
                    {t('sales.jobs.titleAccent')}
                  </span>
                  {runningCount > 0 && (
                    <Badge bg="primary" className="ms-2 fs-6">
                      {t('sales.jobs.runningBadge', { count: runningCount })}
                    </Badge>
                  )}
                </h4>
              </div>
            </Card.Header>
          </Card>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col lg={12}>
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <h5 className="mb-0">
                {t('sales.jobs.titleCount', { count: jobs.length })}
              </h5>
              <Button
                variant="outline-primary"
                size="sm"
                onClick={() => refetch()}
                disabled={isLoading}
              >
                <FontAwesomeIcon
                  icon={faSync}
                  spin={isLoading}
                  className="me-1"
                />{' '}
                {t('sales.jobs.refresh')}
              </Button>
            </Card.Header>
            <Card.Body className="p-0">
              {isLoading && jobs.length === 0 ? (
                <div className="text-center py-5">
                  <Spinner />
                </div>
              ) : jobs.length === 0 ? (
                <div className="text-center text-muted py-5">
                  {t('sales.jobs.empty')}
                </div>
              ) : (
                <Table hover responsive className="mb-0">
                  <thead>
                    <tr>
                      <th></th>
                      <th>{t('sales.jobs.colUrl')}</th>
                      <th>{t('sales.jobs.colStatus')}</th>
                      <th>{t('sales.jobs.colScore')}</th>
                      <th>{t('sales.jobs.colCreated')}</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {jobs.map((job: any) => (
                      <tr
                        key={job.uuid}
                        style={{ cursor: 'pointer' }}
                        onClick={() => navigate(`/sales/jobs/${job.uuid}`)}
                      >
                        <td className="text-center" style={{ width: 30 }}>
                          <FontAwesomeIcon
                            icon={STATUS_ICONS[job.status] || faClock}
                            spin={isRunning(job.status)}
                            className={
                              isRunning(job.status)
                                ? 'text-primary'
                                : STATUS_COLORS[job.status] === 'success'
                                  ? 'text-success'
                                  : 'text-muted'
                            }
                          />
                        </td>
                        <td className="text-truncate" style={{ maxWidth: 250 }}>
                          {job.companyUrl}
                        </td>
                        <td>
                          <Badge bg={STATUS_COLORS[job.status] || 'secondary'}>
                            {job.status}
                          </Badge>
                        </td>
                        <td>
                          {job.totalScore
                            ? t('sales.jobs.scoreWithGrade', {
                                score: job.totalScore,
                                grade: job.grade
                              })
                            : t('sales.jobs.scoreDash')}
                        </td>
                        <td>
                          <small>
                            {new Date(job.createdAt).toLocaleString()}
                          </small>
                        </td>
                        <td>
                          <div className="d-flex gap-1">
                            {(job.status === 'failed' ||
                              job.status === 'cancelled') && (
                              <Button
                                variant="outline-warning"
                                size="sm"
                                onClick={async e => {
                                  e.stopPropagation();
                                  const result = await retryJob(
                                    job.uuid
                                  ).unwrap();
                                  navigate(`/sales/jobs/${result.jobId}`);
                                }}
                              >
                                <FontAwesomeIcon icon={faRedo} />
                              </Button>
                            )}
                            <Button
                              variant="outline-danger"
                              size="sm"
                              onClick={e => {
                                e.stopPropagation();
                                if (
                                  !isRunning(job.status) ||
                                  window.confirm(
                                    t('sales.jobs.confirmCancelRunning')
                                  )
                                ) {
                                  cancelJob(job.uuid);
                                }
                              }}
                            >
                              <FontAwesomeIcon icon={faTrash} />
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              )}
            </Card.Body>
          </Card>
        </Col>
      </Row>
    </>
  );
};

export default JobsPage;
