import { useEffect, useRef, useState } from 'react';
import { Navigate, useBlocker, useParams, useSearchParams } from 'react-router';
import { Alert, Button, Card, Col, Modal, Row, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faTriangleExclamation } from '@fortawesome/free-solid-svg-icons';
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
import LoggingModulePage from '../logging';
import { hasPageRail } from '../configModel';
import { useModuleConfigController } from '../useModuleConfigController';
import { RecordListProvider } from '../recordList/RecordListContext';

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

const GenericModuleDetailPage: React.FC = () => {
  const { t } = useTranslation();
  const { moduleName } = useParams<{ moduleName: string }>();
  const {
    data: mod,
    isLoading,
    error
  } = useGetModuleQuery(moduleName!, { skip: !moduleName });
  const { data: allModules } = useGetModulesQuery();
  const { data: healthData } = useGetModulesHealthQuery();

  // The environment the operator asked for while unsaved edits were pending.
  // Held here until they confirm — see `handleEnvSelect` below.
  const [pendingEnv, setPendingEnv] = useState<string | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  const activeEnv = mod?.activeEnvironment || 'production';
  const environments = mod?.availableEnvironments?.length
    ? mod.availableEnvironments
    : ['production', 'sandbox'];
  // Which profile is on screen lives in the URL beside `?section=`, not in
  // component state. It decides what every field below is bound to, so a link
  // to this page that omits it is a link to a different page than the one the
  // sender was looking at — and "which environment was that screenshot from?"
  // is exactly the question this surface must never leave open. An unknown or
  // stale value falls back to the active profile rather than binding the form
  // to a profile that no longer exists.
  const envParam = searchParams.get('env');
  const currentEnv =
    envParam && environments.includes(envParam) ? envParam : activeEnv;

  const setCurrentEnv = (env: string) => {
    setSearchParams(
      prev => {
        const next = new URLSearchParams(prev);
        next.set('env', env);
        return next;
      },
      { replace: true }
    );
  };

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
    fieldNames,
    secretStatus,
    envLoading,
    saving,
    visibleKeys,
    dirtyCount,
    errorCount,
    perGroup,
    saveBarErrors,
    unfilledByGroup,
    recordList,
    error: saveError,
    success,
    clearError,
    onSave,
    handleDiscard
  } = controller;

  // Full-page rail requires the module to have *declared* configGroups — the
  // legacy heuristic (distinct `field.group` labels with no declared
  // metadata) still promotes ModuleConfigSection's own card-internal rail
  // (`hasCardRail`), but must not promote the whole page. `auth` is the only
  // module in the base that declares a group tree today; for every other one
  // intermixing Overview/Dependencies/Environments around a tree that has no
  // real declared hierarchy would be a bigger change than "no rail".
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

  // Set for exactly one navigation by `confirmEnvSwitch`, which has already
  // asked and already discarded. A ref, not state: the blocker predicate runs
  // synchronously during the navigation `setCurrentEnv` triggers, so a state
  // update queued in the same handler would not be visible yet and the
  // operator would be asked the same question twice.
  const envSwitchConfirmed = useRef(false);

  // Two things discard the form, and both have to prompt.
  //
  // A different pathname is the obvious one. The second is a change to
  // `?env=`: it rebinds every field on the page, and — since the environment
  // moved into the URL — it can now happen without going through the
  // switcher at all. The browser's Back button is enough: it rewrites the
  // query string on the same pathname, the query arg behind
  // `useGetModuleEnvironmentQuery` changes, the form re-seeds, and every
  // unsaved edit is gone with nothing asked. `?section=` must NOT prompt —
  // it only picks which slice of one live form is rendered, and
  // `setSearchParams` is itself a navigation, so a blanket boolean here would
  // fire on every rail click.
  //
  // This is the page's ONLY useBlocker call — see the controller comment
  // above for why that single-registration property matters.
  const blocker = useBlocker(({ currentLocation, nextLocation }) => {
    if (dirtyCount === 0) return false;
    if (currentLocation.pathname !== nextLocation.pathname) return true;
    if (envSwitchConfirmed.current) {
      envSwitchConfirmed.current = false;
      return false;
    }
    const from = new URLSearchParams(currentLocation.search).get('env');
    const to = new URLSearchParams(nextLocation.search).get('env');
    return from !== to;
  });

  // Picking a different environment changes the query arg behind
  // `useGetModuleEnvironmentQuery`, which lands a new `envConfig` and re-seeds
  // the form — silently discarding every unsaved edit across every group.
  // With one form now spanning the whole module and the switcher a rail
  // destination one click from a bar reading "12 unsaved changes", that
  // discard has to be asked for. This handler covers the switcher; the
  // blocker above covers every other route to the same URL (Back, a pasted
  // link, anything that rewrites `?env=`).
  const handleEnvSelect = (env: string) => {
    if (env === currentEnv) return;
    if (dirtyCount > 0) {
      setPendingEnv(env);
      return;
    }
    setCurrentEnv(env);
  };

  const confirmEnvSwitch = () => {
    if (!pendingEnv) return;
    // Discard explicitly rather than leaning on the re-seed effect to do it
    // as a side effect: the operator just agreed to lose these edits, and
    // this way they are gone even if the new environment's payload is
    // already cached and lands with nothing for the effect to react to.
    handleDiscard();
    // The operator has just been asked and agreed; don't ask again when the
    // navigation below reaches the blocker.
    envSwitchConfirmed.current = true;
    setCurrentEnv(pendingEnv);
    setPendingEnv(null);
  };

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

  // A strip, not a badge, and rendered on every section rather than only on
  // the Environments panel. The badge that used to carry this lives in the
  // page header, which scrolls out of view after ~700px — so an operator
  // editing OAuth credentials deep in a long panel had nothing on screen
  // telling them the profile they were about to save is not the live one.
  const inactiveEnvNotice = currentEnv !== activeEnv && (
    <Alert variant="warning" className="fs-10 py-2 mb-3 d-flex gap-2">
      <FontAwesomeIcon icon={faTriangleExclamation} className="mt-1" />
      <span>
        {t('adminModules.detail.env.notActiveWarning', {
          environment: currentEnv,
          active: activeEnv
        })}
      </span>
    </Alert>
  );

  const blockerModal = blocker.state === 'blocked' && (
    <Modal show centered onHide={() => blocker.reset()}>
      <Modal.Header closeButton>
        <Modal.Title>
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

  const envSwitchModal = pendingEnv && (
    <Modal show centered onHide={() => setPendingEnv(null)}>
      <Modal.Header closeButton>
        <Modal.Title>
          {t('adminModules.detail.configCard.unsavedTitle')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body className="fs-10">
        {t('adminModules.detail.configCard.switchEnvBody', {
          environment: pendingEnv
        })}
      </Modal.Body>
      <Modal.Footer>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => setPendingEnv(null)}
        >
          {t('adminModules.detail.configCard.stay')}
        </Button>
        <Button variant="danger" size="sm" onClick={confirmEnvSwitch}>
          {t('adminModules.detail.configCard.switchEnvConfirm')}
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
      <RecordListProvider value={recordList}>
        {blockerModal}
        {envSwitchModal}
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
                onSelect={handleEnvSelect}
              />
            )}

            {inactiveEnvNotice}

            <ModuleConfigSection module={mod} controller={controller} />

            <ModuleDependencyCard module={mod} allModules={allModules} />
          </Col>
        </Row>
      </RecordListProvider>
    );
  }

  const perGroupDisplay = perGroup;
  const saveBarErrorsDisplay = saveBarErrors.map(g => ({
    ...g,
    onSelect: () => setActive(g.key)
  }));
  // A permanently-disabled Save button under Overview/Dependencies/
  // Environments reads as "this panel is a form" when it isn't — only show
  // the bar once there's something it can actually report or act on. A
  // declared parent group with no direct fields of its own — one that exists
  // purely to nest children — is exactly the same situation: its panel is a
  // table of contents, not a form. Gating on `fieldKeys.length`
  // alone isn't enough, though — a leaf node can declare fields that are
  // ALL currently hidden by an unmet `dependsOn` (phase 4's `oauth.google`
  // before either Google enable toggle is on), which is the same "form with
  // nothing to save" situation. `visibleKeys` (module-wide, not node-scoped)
  // is what lets this check tell "owns fields" apart from "owns fields
  // currently on screen".
  const showSaveBar =
    dirtyCount > 0 ||
    errorCount > 0 ||
    Boolean(
      activeNode && activeNode.fieldKeys.some(key => visibleKeys.has(key))
    );

  return (
    <RecordListProvider value={recordList}>
      {blockerModal}
      {envSwitchModal}

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
                statusFor={node => ({
                  unfilled: unfilledByGroup.get(node.key) ?? 0
                })}
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
          {inactiveEnvNotice}

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
                  onSelect={handleEnvSelect}
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
                <div className="pb-4">
                  <ModuleConfigPanel
                    key={activeNode.key}
                    node={activeNode}
                    moduleName={mod.moduleName}
                    schema={schema}
                    control={form.control}
                    register={form.register}
                    fieldNames={fieldNames}
                    secretStatus={secretStatus}
                    onSelectGroup={setActive}
                  />
                </div>
              </Card.Body>
            </Card>
          )}

          {/* Deliberately outside the config card above: the save bar renders
              whenever anything is dirty, from any section, so an operator who
              edits a group, clicks Overview and then Save would otherwise get
              no feedback at all — neither the 400 that lost their config nor
              the confirmation that it landed. These belong to the bar, not to
              the panel, and sit with it. */}
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
    </RecordListProvider>
  );
};

const ModuleDetailPage: React.FC = () => {
  const { moduleName } = useParams<{ moduleName: string }>();

  // Keep specialized workspaces at a component boundary. Each branch owns
  // and executes its own complete hook set, so adding the logging page never
  // makes the generic detail page's hooks conditional.
  if (moduleName === 'logging') {
    return <LoggingModulePage />;
  }

  return <GenericModuleDetailPage />;
};

export default ModuleDetailPage;
