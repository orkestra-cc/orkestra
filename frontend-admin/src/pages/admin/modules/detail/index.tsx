import { useEffect, useMemo, useState } from 'react';
import { Navigate, useBlocker, useParams, useSearchParams } from 'react-router';
import { useWatch } from 'react-hook-form';
import { Alert, Button, Card, Col, Modal, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { OrkestraCardHeader } from 'components/common';
import type { ConfigField } from 'store/api/moduleApi';
import {
  useGetModuleQuery,
  useGetModulesQuery,
  useGetModulesHealthQuery,
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
import ModuleDetailHeader from './ModuleDetailHeader';
import ModuleOverviewPanel from './ModuleOverviewPanel';
import ModuleEnvironmentSwitcher from './ModuleEnvironmentSwitcher';
import ModuleConfigSection from './ModuleConfigSection';
import ModuleConfigRail from './ModuleConfigRail';
import ModuleConfigPanel from './ModuleConfigPanel';
import ModuleDependencyCard from './ModuleDependencyCard';
import ModuleSaveBar from './ModuleSaveBar';
import { buildGroupTree, flattenTree, visibleFields } from '../configModel';
import {
  useModuleConfigForm,
  collectDiff,
  type ConfigFormValues
} from '../useModuleConfigForm';
import { translateConfigGroup } from 'helpers/configLabel';

// Same stable-reference trick as ModuleConfigSection.tsx: the yup-schema memo
// inside useModuleConfigForm keys off `schema` by identity, so a freshly
// minted `[]` on every render (which `mod?.configSchema ?? []` would be
// before the module has loaded) would silently rebuild the validation schema
// on every keystroke. Keeping one module-scope constant here means both this
// component and ModuleConfigSection.tsx hand the hook the exact same stable
// empty array whenever there is nothing better yet.
const EMPTY_SCHEMA: ConfigField[] = [];

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

  // Every hook below must run unconditionally, before either of the early
  // returns further down — that's what makes reading `mod` with `?.` and
  // falling back to EMPTY_SCHEMA/undefined necessary here, even though by
  // the time any of this actually renders, `mod` is guaranteed loaded.
  const { data: envConfig, isLoading: envLoading } =
    useGetModuleEnvironmentQuery(
      { name: mod?.moduleName ?? '', environment: currentEnv },
      { skip: !mod || !mod.availableEnvironments?.length }
    );
  const [updateEnv, { isLoading: saving }] =
    useUpdateModuleEnvironmentMutation();

  const [saveError, setSaveError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const schema = mod?.configSchema ?? EMPTY_SCHEMA;
  const configSource = envConfig?.configValues ?? mod?.configValues;
  const { form, defaults } = useModuleConfigForm(schema, configSource);

  // Re-seed whenever the server-known baseline changes — mirrors
  // ModuleConfigSection.tsx exactly (see that file for the full rationale).
  // Deliberately NOT keyed on `defaults`/`form`, both recomputed every
  // render.
  useEffect(() => {
    form.reset(defaults);
  }, [envConfig, mod?.configValues]);

  const groupTree = useMemo(
    () => buildGroupTree(schema, mod?.configGroups),
    [schema, mod?.configGroups]
  );
  const flatNodes = useMemo(() => flattenTree(groupTree), [groupTree]);

  const secretStatus = envConfig?.secretStatus ?? mod?.secretStatus ?? {};

  const values = useWatch({ control: form.control }) as ConfigFormValues;
  const { errors, dirtyFields } = form.formState;

  // Same "don't useMemo this" reasoning as ModuleConfigSection.tsx: RHF
  // mutates `errors` in place, so a memo would freeze on the first value it
  // ever saw.
  const visibleKeys = new Set(visibleFields(schema, values).map(f => f.key));
  const dirtyKeys = new Set(
    Object.keys(dirtyFields).filter(key => visibleKeys.has(key))
  );
  const errorKeys = new Set(
    Object.keys(errors).filter(key => visibleKeys.has(key))
  );
  const dirtyCount = dirtyKeys.size;
  const errorCount = errorKeys.size;

  // A boolean would block every ?section= switch too, since setSearchParams
  // is itself a navigation — only a real navigation (a different pathname)
  // should ever prompt. Switching sections never trips this.
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

  // Full-page rail requires the module to have *declared* configGroups — the
  // legacy heuristic (distinct `field.group` labels with no declared
  // metadata) still promotes ModuleConfigSection's own card-internal rail,
  // but must not promote the whole page: every module served today declares
  // no configGroups at all, and intermixing Overview/Dependencies/
  // Environments around a tree that has no real declared hierarchy would be
  // a bigger change than "no rail" for those modules. Fewer than 2
  // *top-level* nodes (even when declared) also degrades, matching
  // ModuleConfigSection's own "no rail for a single lone bucket" rule.
  const showRail = Boolean(mod.configGroups?.length) && groupTree.length >= 2;

  if (!showRail) {
    // Today's stacked page, unchanged: header, KPIs, environment switcher,
    // the config card (with its own, decoupled rail if the legacy heuristic
    // finds 2+ buckets), dependencies. This is the path every module
    // currently served takes.
    return (
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

          <ModuleConfigSection module={mod} selectedEnvironment={currentEnv} />

          <ModuleDependencyCard module={mod} allModules={allModules} />
        </Col>
      </Row>
    );
  }

  const selectable = [
    'overview',
    ...flatNodes.map(n => n.key),
    'dependencies',
    // Only a reachable destination when there is actually a choice to make —
    // matches the rail entry itself, and keeps a stale bookmark from landing
    // on a blank pane if the module later drops to one environment.
    ...(environments.length > 1 ? ['environments'] : [])
  ];
  const requested = searchParams.get('section') ?? '';
  const active = selectable.includes(requested) ? requested : 'overview';
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

  const perGroup = flatNodes
    .map(node => ({
      key: node.key,
      label: translateConfigGroup(t, mod.moduleName, node),
      count: node.fieldKeys.filter(k => dirtyKeys.has(k)).length
    }))
    .filter(g => g.count > 0);

  const saveBarErrors = flatNodes
    .map(node => ({
      key: node.key,
      label: translateConfigGroup(t, mod.moduleName, node),
      count: node.fieldKeys.filter(k => errorKeys.has(k)).length
    }))
    .filter(g => g.count > 0)
    .map(g => ({ ...g, onSelect: () => setActive(g.key) }));

  const onSave = async () => {
    const formValues = form.getValues();
    const { config, secrets } = collectDiff(schema, formValues, defaults);
    const keysBeingSaved = [...Object.keys(config), ...Object.keys(secrets)];
    if (keysBeingSaved.length === 0) return;

    // Validate only the fields actually being sent — see
    // ModuleConfigSection.tsx's onSave for the full rationale (a
    // required-but-stored-empty field elsewhere must not block an unrelated
    // save).
    const valid = await form.trigger(keysBeingSaved);
    if (!valid) return;

    setSaveError(null);
    setSuccess(false);

    try {
      await updateEnv({
        name: mod.moduleName,
        environment: currentEnv,
        config: Object.keys(config).length > 0 ? config : undefined,
        secrets: Object.keys(secrets).length > 0 ? secrets : undefined
      }).unwrap();

      // Clear synchronously rather than waiting on the invalidated query's
      // refetch — see ModuleConfigSection.tsx's onSave for why.
      const secretKeys = schema
        .filter(f => f.type === 'secret')
        .map(f => f.key);
      const resetValues: ConfigFormValues = { ...formValues };
      for (const key of secretKeys) resetValues[key] = '';
      form.reset(resetValues);
      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
    } catch (err: unknown) {
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String(
              (err as { data: { detail?: string } }).data?.detail ||
                t('adminModules.detail.configCard.updateFailed')
            )
          : t('adminModules.detail.configCard.updateFailed');
      setSaveError(message);
    }
  };

  const handleDiscard = () => {
    form.reset(defaults);
    setSaveError(null);
    setSuccess(false);
  };

  return (
    <>
      {blocker.state === 'blocked' && (
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
            <Button
              variant="secondary"
              size="sm"
              onClick={() => blocker.reset()}
            >
              {t('adminModules.detail.configCard.stay')}
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => blocker.proceed()}
            >
              {t('adminModules.detail.configCard.leave')}
            </Button>
          </Modal.Footer>
        </Modal>
      )}

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
                    key: 'overview',
                    label: t('adminModules.detail.rail.overview')
                  }
                ]}
                treeCaption={t('adminModules.detail.rail.configuration')}
                trailingCaption={t('adminModules.detail.rail.module')}
                trailingItems={[
                  {
                    key: 'dependencies',
                    label: t('adminModules.detail.rail.dependencies')
                  },
                  ...(environments.length > 1
                    ? [
                        {
                          key: 'environments',
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
          {active === 'overview' && (
            <ModuleOverviewPanel
              module={mod}
              health={health}
              allModules={allModules}
            />
          )}

          {active === 'dependencies' && (
            <ModuleDependencyCard module={mod} allModules={allModules} />
          )}

          {active === 'environments' && environments.length > 1 && (
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
                    onClose={() => setSaveError(null)}
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

          <ModuleSaveBar
            dirtyCount={dirtyCount}
            perGroup={perGroup}
            errorCount={errorCount}
            errors={saveBarErrors}
            saving={saving}
            onDiscard={handleDiscard}
            onSave={onSave}
          />
        </Col>
      </Row>
    </>
  );
};

export default ModuleDetailPage;
