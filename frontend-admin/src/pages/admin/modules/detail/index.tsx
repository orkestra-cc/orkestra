import { useEffect, useState } from 'react';
import { Navigate, useBlocker, useParams, useSearchParams } from 'react-router';
import { Alert, Button, Card, Col, Modal, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { OrkestraCardHeader } from 'components/common';
import {
  useGetModuleQuery,
  useGetModulesQuery,
  useGetModulesHealthQuery
} from 'store/api/moduleApi';
import ModuleDetailHeader from './ModuleDetailHeader';
import ModuleOverviewPanel from './ModuleOverviewPanel';
import ModuleEnvironmentSwitcher from './ModuleEnvironmentSwitcher';
import ModuleConfigSection from './ModuleConfigSection';
import ModuleConfigRail from './ModuleConfigRail';
import ModuleConfigPanel from './ModuleConfigPanel';
import ModuleDependencyCard from './ModuleDependencyCard';
import ModuleSaveBar from './ModuleSaveBar';
import { hasPageRail } from '../configModel';
import { useModuleConfigController } from '../useModuleConfigController';

// The rail's built-in, non-config entries. Reserved (double-underscore)
// rather than the plain words a first draft used ('overview', 'dependencies',
// 'environments') — a backend module is free to key a declared config group
// however it likes, and a group literally keyed 'dependencies' would
// otherwise resolve to the SAME `active` value as the built-in Dependencies
// panel, rendering both stacked on top of each other. Prefixing these three
// makes that collision structurally impossible instead of merely unlikely.
const SECTION_OVERVIEW = '__overview';
const SECTION_DEPENDENCIES = '__dependencies';
const SECTION_ENVIRONMENTS = '__environments';

const ModuleDetailPage: React.FC = () => {
  const { t } = useTranslation();
  const { moduleName } = useParams<{ moduleName: string }>();
  const {
    data: mod,
    isLoading,
    error
  } = useGetModuleQuery(moduleName!, { skip: !moduleName });
  const { data: allModules } = useGetModulesQuery();
  const { data: healthData } = useGetModulesHealthQuery();

  const [selectedEnv, setSelectedEnv] = useState<string | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  const activeEnv = mod?.activeEnvironment || 'production';
  const environments = mod?.availableEnvironments?.length
    ? mod.availableEnvironments
    : ['production', 'sandbox'];
  const currentEnv = selectedEnv || activeEnv;

  // The ONE controller instance for this module's whole config surface.
  // Both branches below — the degradation stacked page (which hands this to
  // ModuleConfigSection as a prop) and the full-page rail (which reads it
  // directly) — share this single instance rather than each creating their
  // own. That is load-bearing, not cosmetic: react-router supports exactly
  // one useBlocker registration at a time (a second silently wins over the
  // first — see the blocker below), so if ModuleConfigSection also created
  // its own controller+blocker, whichever registered last would decide
  // whether unsaved-changes protection exists at all, and that outcome
  // depended on RTK Query cache warmth rather than on any real state. With
  // one shared instance there is nothing left to race.
  const controller = useModuleConfigController(mod, currentEnv);
  const {
    schema,
    groupTree,
    flatNodes,
    form,
    secretStatus,
    envLoading,
    saving,
    dirtyCount,
    errorCount,
    perGroup,
    saveBarErrors,
    error: saveError,
    success,
    clearError,
    onSave,
    handleDiscard
  } = controller;

  // Full-page rail requires the module to have *declared* configGroups — the
  // legacy heuristic (distinct `field.group` labels with no declared
  // metadata) still promotes ModuleConfigSection's own card-internal rail
  // (`hasCardRail`), but must not promote the whole page: every module
  // served today declares no configGroups at all, and intermixing Overview/
  // Dependencies/Environments around a tree that has no real declared
  // hierarchy would be a bigger change than "no rail" for those modules.
  const showRail = hasPageRail(groupTree, mod?.configGroups);

  const selectable = [
    SECTION_OVERVIEW,
    ...flatNodes.map(n => n.key),
    SECTION_DEPENDENCIES,
    // Only a reachable destination when there is actually a choice to make —
    // matches the rail entry itself, and keeps a stale bookmark from landing
    // on a blank pane if the module later drops to one environment.
    ...(environments.length > 1 ? [SECTION_ENVIRONMENTS] : [])
  ];
  const requested = searchParams.get('section') ?? '';
  const active = selectable.includes(requested) ? requested : SECTION_OVERVIEW;
  const activeNode = flatNodes.find(node => node.key === active);

  const setActive = (key: string) => {
    setSearchParams(
      prev => {
        const next = new URLSearchParams(prev);
        next.set('section', key);
        return next;
      },
      { replace: true }
    );
  };

  // A stale/unknown ?section= (a renamed or removed group, a copied link
  // from before a module's groups changed) already falls back to Overview
  // for rendering above — but left as-is in the address bar, that same dead
  // value keeps propagating every time the link is shared again. Rewrite it
  // once resolved, still with `replace` so this never adds a history entry.
  useEffect(() => {
    if (showRail && requested && requested !== active) {
      setActive(active);
    }
  }, [showRail, requested, active]);

  // A boolean would block every ?section= switch too, since setSearchParams
  // is itself a navigation — only a real navigation (a different pathname)
  // should ever prompt. Switching sections never trips this. This is the
  // page's ONLY useBlocker call — see the controller comment above for why
  // that single-registration property matters.
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirtyCount > 0 && currentLocation.pathname !== nextLocation.pathname
  );

  if (!moduleName) return <Navigate to="/admin/modules" replace />;
  if (isLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" />
      </div>
    );
  }
  if (error || !mod) {
    return <Navigate to="/admin/modules" replace />;
  }

  const health = healthData?.modules.find(h => h.moduleName === moduleName);

  const blockerModal = blocker.state === 'blocked' && (
    <Modal show centered onHide={() => blocker.reset()}>
      <Modal.Header closeButton>
        <Modal.Title className="fs-8">
          {t('adminModules.detail.configCard.unsavedTitle')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body className="fs-10">
        {t('adminModules.detail.configCard.unsavedBody')}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" size="sm" onClick={() => blocker.reset()}>
          {t('adminModules.detail.configCard.stay')}
        </Button>
        <Button variant="danger" size="sm" onClick={() => blocker.proceed()}>
          {t('adminModules.detail.configCard.leave')}
        </Button>
      </Modal.Footer>
    </Modal>
  );

  if (!showRail) {
    // Today's stacked page, unchanged: header, KPIs, environment switcher,
    // the config card (with its own, decoupled rail if the legacy heuristic
    // finds 2+ buckets), dependencies. This is the path every module
    // currently served takes. The shared controller (and its blocker,
    // registered once above) still drives ModuleConfigSection here — it no
    // longer creates its own.
    return (
      <>
        {blockerModal}
        <Row className="g-3">
          <Col xxl={12}>
            <ModuleDetailHeader module={mod} />

            <ModuleOverviewPanel
              module={mod}
              health={health}
              allModules={allModules}
            />

            {environments.length > 1 && (
              <ModuleEnvironmentSwitcher
                moduleName={mod.moduleName}
                activeEnvironment={activeEnv}
                availableEnvironments={environments}
                selectedEnvironment={currentEnv}
                onSelect={setSelectedEnv}
              />
            )}

            <ModuleConfigSection module={mod} controller={controller} />

            <ModuleDependencyCard module={mod} allModules={allModules} />
          </Col>
        </Row>
      </>
    );
  }

  const perGroupDisplay = perGroup;
  const saveBarErrorsDisplay = saveBarErrors.map(g => ({
    ...g,
    onSelect: () => setActive(g.key)
  }));
  // A permanently-disabled Save button under Overview/Dependencies/
  // Environments reads as "this panel is a form" when it isn't — only show
  // the bar once there's something it can actually report or act on.
  const showSaveBar = dirtyCount > 0 || errorCount > 0 || Boolean(activeNode);

  return (
    <>
      {blockerModal}

      <ModuleDetailHeader module={mod} />

      <Row className="g-3">
        <Col md={4} lg={3}>
          <Card className="shadow-none border mb-3">
            <Card.Body className="p-2">
              <ModuleConfigRail
                tree={groupTree}
                moduleName={mod.moduleName}
                activeKey={active}
                onSelect={setActive}
                statusFor={() => ({ unfilled: 0 })}
                leadingItems={[
                  {
                    key: SECTION_OVERVIEW,
                    label: t('adminModules.detail.rail.overview')
                  }
                ]}
                treeCaption={t('adminModules.detail.rail.configuration')}
                trailingCaption={t('adminModules.detail.rail.module')}
                trailingItems={[
                  {
                    key: SECTION_DEPENDENCIES,
                    label: t('adminModules.detail.rail.dependencies')
                  },
                  ...(environments.length > 1
                    ? [
                        {
                          key: SECTION_ENVIRONMENTS,
                          label: t('adminModules.detail.rail.environments')
                        }
                      ]
                    : [])
                ]}
              />
            </Card.Body>
          </Card>
        </Col>

        <Col md={8} lg={9}>
          {active === SECTION_OVERVIEW && (
            <ModuleOverviewPanel
              module={mod}
              health={health}
              allModules={allModules}
            />
          )}

          {active === SECTION_DEPENDENCIES && (
            <ModuleDependencyCard module={mod} allModules={allModules} />
          )}

          {active === SECTION_ENVIRONMENTS && environments.length > 1 && (
            <Card className="mb-3">
              <OrkestraCardHeader
                title={t('adminModules.detail.rail.environments')}
                light={false}
              />
              <Card.Body>
                <ModuleEnvironmentSwitcher
                  moduleName={mod.moduleName}
                  activeEnvironment={activeEnv}
                  availableEnvironments={environments}
                  selectedEnvironment={currentEnv}
                  onSelect={setSelectedEnv}
                />
              </Card.Body>
            </Card>
          )}

          {activeNode && (
            <Card className="mb-3">
              <OrkestraCardHeader
                title={t('adminModules.detail.cards.configuration')}
                light={false}
                endEl={
                  envLoading ? (
                    <Spinner animation="border" size="sm" />
                  ) : undefined
                }
              />
              <Card.Body>
                {saveError && (
                  <Alert
                    variant="danger"
                    className="fs-10"
                    dismissible
                    onClose={clearError}
                  >
                    {saveError}
                  </Alert>
                )}
                {success && (
                  <Alert variant="success" className="fs-10">
                    {t('adminModules.detail.configCard.saved')}
                  </Alert>
                )}
                <div className="pb-4">
                  <ModuleConfigPanel
                    key={activeNode.key}
                    node={activeNode}
                    moduleName={mod.moduleName}
                    schema={schema}
                    control={form.control}
                    register={form.register}
                    secretStatus={secretStatus}
                  />
                </div>
              </Card.Body>
            </Card>
          )}

          {showSaveBar && (
            <ModuleSaveBar
              dirtyCount={dirtyCount}
              perGroup={perGroupDisplay}
              errorCount={errorCount}
              errors={saveBarErrorsDisplay}
              saving={saving}
              onDiscard={handleDiscard}
              onSave={onSave}
            />
          )}
        </Col>
      </Row>
    </>
  );
};

export default ModuleDetailPage;
