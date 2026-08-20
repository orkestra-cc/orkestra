import { useEffect } from 'react';
import { Alert, Card, Col, Row, Spinner } from 'react-bootstrap';
import { Navigate, useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  useGetModuleQuery,
  useGetModulesHealthQuery,
  useGetModulesQuery
} from 'store/api/moduleApi';
import { useGetLogLevelsQuery } from 'store/api/observabilityApi';
import ModuleConfigRail from '../detail/ModuleConfigRail';
import ModuleDependencyCard from '../detail/ModuleDependencyCard';
import ModuleDetailHeader from '../detail/ModuleDetailHeader';
import ModuleOverviewPanel from '../detail/ModuleOverviewPanel';
import DiagnosticsPanel from './DiagnosticsPanel';
import LoggingOverview from './LoggingOverview';
import LogPreviewPanel from './LogPreviewPanel';
import PermanentLevelsPanel from './PermanentLevelsPanel';

type LoggingSection = 'overview' | 'levels' | 'diagnostics' | 'logs';

const VALID_SECTIONS: LoggingSection[] = [
  'overview',
  'levels',
  'diagnostics',
  'logs'
];

export const LoggingModulePage = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedSection = searchParams.get('section');
  const activeSection: LoggingSection = VALID_SECTIONS.includes(
    requestedSection as LoggingSection
  )
    ? (requestedSection as LoggingSection)
    : 'overview';

  const {
    data: module,
    isLoading: moduleLoading,
    error: moduleError
  } = useGetModuleQuery('logging');
  const { data: allModules } = useGetModulesQuery();
  const { data: healthData } = useGetModulesHealthQuery();
  const {
    data: snapshot,
    isLoading: snapshotLoading,
    error: snapshotError,
    refetch: refetchSnapshot
  } = useGetLogLevelsQuery();

  const setActiveSection = (section: string) => {
    if (!VALID_SECTIONS.includes(section as LoggingSection)) return;
    setSearchParams(
      previous => {
        const next = new URLSearchParams(previous);
        next.set('section', section);
        return next;
      },
      { replace: true }
    );
  };

  // Rendering already falls back safely, but stale bookmarks would keep
  // propagating the dead section value. URL synchronization is an external
  // router side effect, so it belongs in this focused effect.
  useEffect(() => {
    if (requestedSection && requestedSection !== activeSection) {
      setActiveSection(activeSection);
    }
  }, [requestedSection, activeSection]);

  if (moduleLoading || snapshotLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" />
      </div>
    );
  }

  if (moduleError || !module) {
    return <Navigate to="/admin/modules" replace />;
  }

  const health = healthData?.modules.find(
    entry => entry.moduleName === 'logging'
  );

  return (
    <>
      <ModuleDetailHeader module={module} />

      <Row className="g-3">
        <Col md={4} lg={3}>
          <Card className="shadow-none border mb-3">
            <Card.Body className="p-2">
              <ModuleConfigRail
                tree={[]}
                moduleName="logging"
                activeKey={activeSection}
                onSelect={setActiveSection}
                statusFor={() => ({ unfilled: 0 })}
                leadingItems={[
                  {
                    key: 'overview',
                    label: t(
                      'adminObservability.loggingWorkspace.sections.overview'
                    )
                  }
                ]}
                trailingCaption={t(
                  'adminObservability.loggingWorkspace.sections.operations'
                )}
                trailingItems={[
                  {
                    key: 'levels',
                    label: t(
                      'adminObservability.loggingWorkspace.sections.levels'
                    )
                  },
                  {
                    key: 'diagnostics',
                    label: t(
                      'adminObservability.loggingWorkspace.sections.diagnostics'
                    )
                  },
                  {
                    key: 'logs',
                    label: t(
                      'adminObservability.loggingWorkspace.sections.logs'
                    )
                  }
                ]}
              />
            </Card.Body>
          </Card>
        </Col>

        <Col md={8} lg={9}>
          {snapshotError || !snapshot ? (
            <Alert variant="danger" className="fs-10">
              {t('adminObservability.loggingWorkspace.loadFailed')}
            </Alert>
          ) : (
            <>
              {activeSection === 'overview' && (
                <>
                  <LoggingOverview snapshot={snapshot} />
                  <ModuleOverviewPanel
                    module={module}
                    health={health}
                    allModules={allModules}
                  />
                  <ModuleDependencyCard
                    module={module}
                    allModules={allModules}
                  />
                </>
              )}

              {activeSection === 'levels' && (
                <PermanentLevelsPanel
                  key={snapshot.updatedAt}
                  snapshot={snapshot}
                  onReload={async () => {
                    await refetchSnapshot();
                  }}
                />
              )}

              {activeSection === 'diagnostics' && (
                <DiagnosticsPanel snapshot={snapshot} />
              )}

              {activeSection === 'logs' && (
                <LogPreviewPanel snapshot={snapshot} />
              )}
            </>
          )}
        </Col>
      </Row>
    </>
  );
};

export default LoggingModulePage;
