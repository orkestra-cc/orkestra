import { useCallback, useEffect } from 'react';
import { Alert, Spinner } from 'react-bootstrap';
import { faListCheck } from '@fortawesome/free-solid-svg-icons';
import { Link, Navigate, useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import SectionCard from 'components/common/SectionCard';
import { useListJobsQuery } from 'store/api/jobsApi';
import { useGetModuleQuery } from 'store/api/moduleApi';
import ModuleDetailHeader from '../detail/ModuleDetailHeader';
import JobDrawer from './JobDrawer';
import JobsTable, { type JobDrawerTab } from './JobsTable';
import { useJobActions } from './useJobActions';

const TABS: JobDrawerTab[] = ['overview', 'schedule', 'runs'];
const POLL_MS = 15000;

// The jobs workspace: a specialized /admin/modules/jobs page (like logging)
// instead of the generic config detail. The selected job and the drawer tab
// live in the URL (?job=<key>&tab=overview|schedule|runs) so a view can be
// shared; `?view=settings` hands over to the generic detail page, where the
// module's own configuration (timezone, retention) is edited.
export const JobsModulePage = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedKey = searchParams.get('job');
  const requestedTab = searchParams.get('tab');
  const tab: JobDrawerTab = TABS.includes(requestedTab as JobDrawerTab)
    ? (requestedTab as JobDrawerTab)
    : 'overview';

  const {
    data: module,
    isLoading: moduleLoading,
    error: moduleError
  } = useGetModuleQuery('jobs');
  const jobsQuery = useListJobsQuery(undefined, { pollingInterval: POLL_MS });
  const actions = useJobActions();

  const open = useCallback(
    (key: string, nextTab: JobDrawerTab) =>
      setSearchParams(
        previous => {
          const next = new URLSearchParams(previous);
          next.set('job', key);
          next.set('tab', nextTab);
          return next;
        },
        { replace: true }
      ),
    [setSearchParams]
  );

  const setTab = (nextTab: JobDrawerTab) =>
    setSearchParams(
      previous => {
        const next = new URLSearchParams(previous);
        next.set('tab', nextTab);
        return next;
      },
      { replace: true }
    );

  const close = () =>
    setSearchParams(
      previous => {
        const next = new URLSearchParams(previous);
        next.delete('job');
        next.delete('tab');
        return next;
      },
      { replace: true }
    );

  const jobs = jobsQuery.data;
  const selected = jobs?.find(job => job.key === selectedKey);

  // A bookmarked key that no longer exists (module removed its job) must not
  // keep propagating through the URL.
  useEffect(() => {
    if (selectedKey && jobs && !selected) {
      setSearchParams(
        previous => {
          const next = new URLSearchParams(previous);
          next.delete('job');
          next.delete('tab');
          return next;
        },
        { replace: true }
      );
    }
  }, [selectedKey, jobs, selected, setSearchParams]);

  if (moduleLoading) {
    return (
      <div
        className="text-center py-5"
        role="status"
        aria-label={t('adminJobs.loadingAria')}
      >
        <Spinner animation="border" />
        <span className="visually-hidden">{t('adminJobs.loading')}</span>
      </div>
    );
  }
  if (moduleError || !module) {
    return <Navigate to="/admin/modules" replace />;
  }

  return (
    <>
      <ModuleDetailHeader module={module} />
      <SectionCard
        icon={faListCheck}
        title={t('adminJobs.title')}
        subtitle={t('adminJobs.subtitle')}
        bodyClassName="p-0"
        headerEnd={
          <Link to="?view=settings" className="btn btn-orkestra-default btn-sm">
            {t('adminJobs.moduleSettings')}
          </Link>
        }
      >
        {jobsQuery.isLoading ? (
          <div className="text-center py-4" role="status">
            <Spinner animation="border" size="sm" className="me-2" />
            <span className="fs-10">{t('adminJobs.loadingJobs')}</span>
          </div>
        ) : jobsQuery.error || !jobs ? (
          <Alert variant="danger" className="fs-10 m-3">
            {t('adminJobs.loadFailed')}
          </Alert>
        ) : jobs.length === 0 ? (
          <p className="text-600 fs-10 m-3" role="status">
            {t('adminJobs.empty')}
          </p>
        ) : (
          <JobsTable jobs={jobs} actions={actions} onOpen={open} />
        )}
      </SectionCard>
      {selected && (
        <JobDrawer
          key={selected.key}
          job={selected}
          tab={tab}
          actions={actions}
          onTab={setTab}
          onClose={close}
        />
      )}
    </>
  );
};

export default JobsModulePage;
