import { Alert, Button, Nav, Offcanvas, Tab } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import type { Job } from 'types/jobs';
import type { JobDrawerTab } from './JobsTable';
import RunsPanel from './RunsPanel';
import ScheduleEditor from './ScheduleEditor';
import { describeSchedule, formatDuration, jobStatus } from './format';
import type { JobActions } from './useJobActions';

interface JobDrawerProps {
  job: Job;
  tab: JobDrawerTab;
  actions: JobActions;
  onTab: (tab: JobDrawerTab) => void;
  onClose: () => void;
}

const Row = ({
  label,
  children
}: {
  label: string;
  children: React.ReactNode;
}) => (
  <>
    <dt className="col-sm-4 text-600 fw-normal">{label}</dt>
    <dd className="col-sm-8 text-break">{children}</dd>
  </>
);

const Overview = ({ job }: { job: Job }) => {
  const { t } = useTranslation();
  const status = jobStatus(job);
  return (
    <>
      {job.description && <p className="fs-10">{job.description}</p>}
      <dl className="row fs-10 mb-0">
        <Row label={t('adminJobs.overview.key')}>
          <code>{job.key}</code>
        </Row>
        <Row label={t('adminJobs.columns.module')}>{job.module}</Row>
        <Row label={t('adminJobs.columns.status')}>
          <SubtleBadge bg={status.color}>
            {t(`adminJobs.status.${status.key}`)}
          </SubtleBadge>
        </Row>
        <Row label={t('adminJobs.columns.schedule')}>
          {describeSchedule(t, job.schedule, job.timezone)}
        </Row>
        <Row label={t('adminJobs.overview.declared')}>
          {describeSchedule(t, job.defaultSchedule, job.timezone)}
          {!job.defaultEnabled && ` · ${t('adminJobs.overview.offByDefault')}`}
        </Row>
        <Row label={t('adminJobs.columns.nextRun')}>
          {formatDateTime(job.nextRunAt)}
        </Row>
        {job.retryAt && (
          <Row label={t('adminJobs.overview.retryAt')}>
            {formatDateTime(job.retryAt)} ·{' '}
            {t('adminJobs.overview.attempt', { count: job.attempt })}
          </Row>
        )}
        {job.runRequestedAt && (
          <Row label={t('adminJobs.overview.runRequested')}>
            {formatDateTime(job.runRequestedAt)}
          </Row>
        )}
        <Row label={t('adminJobs.overview.timeout')}>
          {formatDuration(job.timeoutSeconds * 1000)}
        </Row>
        <Row label={t('adminJobs.overview.maxAttempts')}>{job.maxAttempts}</Row>
        <Row label={t('adminJobs.overview.revision')}>{job.revision}</Row>
      </dl>
      {job.lastRun && (
        <div className="mt-3">
          <div className="fw-semibold fs-10 mb-1">
            {t('adminJobs.overview.lastRun')}
          </div>
          <div className="fs-10">
            {t(`adminJobs.runs.status.${job.lastRun.status}`)} ·{' '}
            {formatDateTime(job.lastRun.startedAt)} ·{' '}
            {formatDuration(job.lastRun.durationMs)}
          </div>
          {job.lastRun.error && (
            <pre className="bg-body-tertiary border rounded p-2 mt-2 mb-0 fs-11 text-wrap text-break">
              {job.lastRun.error}
            </pre>
          )}
        </div>
      )}
    </>
  );
};

const JobDrawer = ({ job, tab, actions, onTab, onClose }: JobDrawerProps) => {
  const { t } = useTranslation();
  const titleId = `job-drawer-title-${job.key}`;
  // Editing is a manage-only tab; a read-only operator never sees it, and a
  // bookmarked ?tab=schedule falls back to the overview.
  const tabs: JobDrawerTab[] = actions.canManage
    ? ['overview', 'schedule', 'runs']
    : ['overview', 'runs'];
  const active = tabs.includes(tab) ? tab : 'overview';

  return (
    <Offcanvas
      show
      placement="end"
      onHide={onClose}
      aria-labelledby={titleId}
      style={{ width: 'min(640px, 100vw)' }}
    >
      <Offcanvas.Header closeButton>
        <Offcanvas.Title as="h2" id={titleId} className="fs-8">
          {job.title}
        </Offcanvas.Title>
      </Offcanvas.Header>
      <Offcanvas.Body>
        <Tab.Container
          activeKey={active}
          onSelect={key => key && onTab(key as JobDrawerTab)}
        >
          <Nav variant="tabs" className="mb-3">
            {tabs.map(key => (
              <Nav.Item key={key}>
                <Nav.Link eventKey={key}>{t(`adminJobs.tabs.${key}`)}</Nav.Link>
              </Nav.Item>
            ))}
          </Nav>
          <Tab.Content>
            <Tab.Pane eventKey="overview" mountOnEnter unmountOnExit>
              <Overview job={job} />
            </Tab.Pane>
            {actions.canManage && (
              <Tab.Pane eventKey="schedule" mountOnEnter unmountOnExit>
                <ScheduleEditor job={job} actions={actions} />
                {job.overridden && (
                  <div className="mt-3 pt-3 border-top">
                    <Alert variant="info" className="fs-10 py-2">
                      {t('adminJobs.editor.overriddenNote')}
                    </Alert>
                    <Button
                      variant="orkestra-default"
                      size="sm"
                      disabled={actions.busyKey === job.key}
                      onClick={() => actions.reset(job)}
                    >
                      {t('adminJobs.editor.restoreDefaults')}
                    </Button>
                  </div>
                )}
              </Tab.Pane>
            )}
            <Tab.Pane eventKey="runs" mountOnEnter unmountOnExit>
              <RunsPanel jobKey={job.key} />
            </Tab.Pane>
          </Tab.Content>
        </Tab.Container>
      </Offcanvas.Body>
    </Offcanvas>
  );
};

export default JobDrawer;
