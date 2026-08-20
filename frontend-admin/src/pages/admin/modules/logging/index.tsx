import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Card, Col, Modal, Row, Spinner } from 'react-bootstrap';
import { Navigate, useBlocker, useSearchParams } from 'react-router';
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
import PermanentLevelsPanel, {
  countChanges,
  editorFromSnapshot,
  type PermanentEditor
} from './PermanentLevelsPanel';

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
  const [permanentEditor, setPermanentEditor] =
    useState<PermanentEditor | null>(null);

  // The server snapshot is external state. Seed the editor when it first
  // arrives and accept later snapshots only while the editor is clean; a
  // refetch must never overwrite an operator's draft or conflict recovery.
  useEffect(() => {
    if (!snapshot) return;
    setPermanentEditor(current => {
      if (!current) return editorFromSnapshot(snapshot);
      if (current.conflict) return current;
      if (countChanges(current.baseline, current.draft) > 0) return current;
      if (current.expectedPermanentRevision === snapshot.permanentRevision) {
        return current;
      }
      return editorFromSnapshot(snapshot);
    });
  }, [snapshot]);

  const dirtyCount = permanentEditor
    ? countChanges(permanentEditor.baseline, permanentEditor.draft)
    : 0;

  // Section switches keep one editor mounted and are safe. Only leaving this
  // route would discard a draft, so mirror the generic module detail guard.
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirtyCount > 0 && currentLocation.pathname !== nextLocation.pathname
  );

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
    if (requestedSection !== null && requestedSection !== activeSection) {
      setActiveSection(activeSection);
    }
  }, [requestedSection, activeSection]);

  const reloadSnapshot = useCallback(async () => {
    return refetchSnapshot().unwrap();
  }, [refetchSnapshot]);

  if (moduleLoading || snapshotLoading) {
    return (
      <div
        className="text-center py-5"
        role="status"
        aria-label={t('adminObservability.loggingWorkspace.loadingAria')}
      >
        <Spinner animation="border" />
        <span className="visually-hidden">
          {t('adminObservability.loggingWorkspace.loading')}
        </span>
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
      {blocker.state === 'blocked' && (
        <Modal show centered onHide={() => blocker.reset()}>
          <Modal.Header closeButton>
            <Modal.Title as="h2">
              {t('adminObservability.loggingWorkspace.unsaved.title')}
            </Modal.Title>
          </Modal.Header>
          <Modal.Body className="fs-10">
            {t('adminObservability.loggingWorkspace.unsaved.body')}
          </Modal.Body>
          <Modal.Footer>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => blocker.reset()}
            >
              {t('adminObservability.loggingWorkspace.unsaved.stay')}
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => blocker.proceed()}
            >
              {t('adminObservability.loggingWorkspace.unsaved.leave')}
            </Button>
          </Modal.Footer>
        </Modal>
      )}
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

              {activeSection === 'levels' && permanentEditor && (
                <PermanentLevelsPanel
                  snapshot={snapshot}
                  editor={permanentEditor}
                  setEditor={setPermanentEditor}
                  onReload={reloadSnapshot}
                />
              )}

              {activeSection === 'diagnostics' && (
                <DiagnosticsPanel
                  snapshot={snapshot}
                  onDiagnosticsExpired={reloadSnapshot}
                />
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
