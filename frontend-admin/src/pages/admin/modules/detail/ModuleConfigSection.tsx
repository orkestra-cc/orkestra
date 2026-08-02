import { useEffect, useMemo, useState } from 'react';
import { useBlocker } from 'react-router';
import { useWatch } from 'react-hook-form';
import { Alert, Card, Col, Modal, Button, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { OrkestraCardHeader } from 'components/common';
import type { ModuleConfig, ConfigField } from 'store/api/moduleApi';
import {
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
import ModuleConfigFields from '../ModuleConfigFields';
import { buildGroupTree, flattenTree, visibleFields } from '../configModel';
import {
  useModuleConfigForm,
  collectDiff,
  type ConfigFormValues
} from '../useModuleConfigForm';
import { translateConfigGroup } from 'helpers/configLabel';
import ModuleConfigRail from './ModuleConfigRail';
import ModuleConfigPanel from './ModuleConfigPanel';
import ModuleSaveBar from './ModuleSaveBar';

interface ModuleConfigSectionProps {
  module: ModuleConfig;
  selectedEnvironment: string;
}

// A stable empty-array reference for modules with no declared schema. The
// yup-schema memo inside useModuleConfigForm keys off `schema` by identity —
// `mod.configSchema ?? []` would mint a brand-new array every render (and so
// would `.filter()`/`.sort()`/a spread), silently rebuilding the whole
// validation schema on every keystroke instead of once.
const EMPTY_SCHEMA: ConfigField[] = [];

const ModuleConfigSection: React.FC<ModuleConfigSectionProps> = ({
  module: mod,
  selectedEnvironment
}) => {
  const { t } = useTranslation();
  const { data: envConfig, isLoading: envLoading } =
    useGetModuleEnvironmentQuery(
      { name: mod.moduleName, environment: selectedEnvironment },
      { skip: !mod.availableEnvironments?.length }
    );

  const [updateEnv, { isLoading: saving }] =
    useUpdateModuleEnvironmentMutation();

  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [activeKey, setActiveKey] = useState('');

  const schema = mod.configSchema ?? EMPTY_SCHEMA;
  // The fetched environment wins once it has loaded; before that (or for a
  // module with no declared environments, which skips the query above) the
  // module-list snapshot is the best available baseline.
  const configSource = envConfig?.configValues ?? mod.configValues;
  const { form, defaults } = useModuleConfigForm(schema, configSource);

  // Re-seed the form whenever the server-known baseline changes: the initial
  // environment fetch resolving, switching environments, or (matching the
  // pre-RHF behaviour this replaces) a fresh `mod.configValues` reference
  // from the parent. Deliberately NOT keyed on `defaults`/`form` — both are
  // recomputed every render, and depending on `defaults` would reset on
  // every keystroke, discarding the very edits the sticky bar exists to
  // accumulate across groups.
  useEffect(() => {
    form.reset(defaults);
  }, [envConfig, mod.configValues]);

  const groupTree = useMemo(
    () => buildGroupTree(schema, mod.configGroups),
    [schema, mod.configGroups]
  );
  // Legacy heuristic (no declared groups): only show the rail once there are
  // ≥2 synthesized buckets — a single bucket would be a redundant one-entry
  // rail. That threshold matched `bucketByGroup`'s caller and must keep
  // matching it exactly (no module declares groups today, so this is the
  // only branch any current module hits). A module that *does* declare
  // configGroups opted into an explicit rail, so it always gets one, even a
  // lone top-level entry — that entry can still have nested children, which
  // the legacy flat-bucket case never has.
  const showRail = groupTree.length >= 2 || Boolean(mod.configGroups?.length);
  const flatNodes = useMemo(() => flattenTree(groupTree), [groupTree]);
  const currentKey = activeKey || flatNodes[0]?.key || '';
  // A key can survive in state past a structural group change (module
  // config changed shape) and no longer match anything in the current tree.
  // Falling back to the first node keeps the rail and panel in sync instead
  // of silently dropping to the unscoped flat form below.
  const activeNode =
    flatNodes.find(node => node.key === currentKey) ?? flatNodes[0];

  const secretStatus = envConfig?.secretStatus ?? mod.secretStatus ?? {};

  // Live values for visibility (dependsOn can reference a field in a
  // different group than the one currently on screen) and for the save
  // bar's cross-group aggregation below.
  const values = useWatch({ control: form.control }) as ConfigFormValues;
  const { errors, isDirty, dirtyFields } = form.formState;

  // Deliberately NOT useMemo here: react-hook-form mutates its `errors`
  // object in place (same reference across renders even as its content
  // changes), so a memo keyed on `[errors, ...]` silently freezes on the
  // first value it ever saw — `dirtyFields` happens to get a fresh object
  // per update and would "work", but the same pattern is a trap the moment
  // errors are involved. These are O(field count) list operations on at
  // most a few dozen entries, cheap enough to just recompute every render.
  const visibleKeys = new Set(visibleFields(schema, values).map(f => f.key));
  // RHF's own dirty/error tracking doesn't know about `dependsOn` — a hidden
  // field the operator can't see and can't reach must not inflate the
  // "unsaved changes" count or claim a rail entry, matching `collectDiff`'s
  // own hidden-field exclusion.
  const dirtyKeys = new Set(
    Object.keys(dirtyFields).filter(key => visibleKeys.has(key))
  );
  const errorKeys = new Set(
    Object.keys(errors).filter(key => visibleKeys.has(key))
  );

  // Every field belongs to exactly one node's `fieldKeys` (buildGroupTree
  // assigns by exact `group` match, never to an ancestor), so summing per
  // node reproduces the total with no double-counting.
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
    .map(g => ({ ...g, onSelect: () => setActiveKey(g.key) }));

  const dirtyCount = dirtyKeys.size;

  const onSave = form.handleSubmit(async formValues => {
    const { config, secrets } = collectDiff(schema, formValues, defaults);
    if (Object.keys(config).length === 0 && Object.keys(secrets).length === 0) {
      return;
    }

    setError(null);
    setSuccess(false);

    try {
      await updateEnv({
        name: mod.moduleName,
        environment: selectedEnvironment,
        config: Object.keys(config).length > 0 ? config : undefined,
        secrets: Object.keys(secrets).length > 0 ? secrets : undefined
      }).unwrap();

      // Clears the bar immediately instead of waiting on the invalidated
      // query to refetch. Secret fields go along for the ride here (they
      // hold whatever the operator just typed) but the effect above will
      // collapse them back to '' the moment the refetch lands, matching
      // buildDefaults' "a secret always starts empty" rule.
      form.reset(formValues);
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
      setError(message);
    }
  });

  const handleDiscard = () => {
    form.reset(defaults);
    setError(null);
    setSuccess(false);
  };

  // Block navigation when there are unsaved changes.
  const blocker = useBlocker(isDirty);

  if (schema.length === 0) {
    return (
      <Card className="mb-3">
        <OrkestraCardHeader
          title={t('adminModules.detail.cards.configuration')}
          light={false}
        />
        <Card.Body className="text-muted text-center py-4 fs-10">
          {t('adminModules.detail.configCard.noSettings')}
        </Card.Body>
      </Card>
    );
  }

  const renderFields = (keys?: string[]) => (
    <ModuleConfigFields
      schema={schema}
      includeKeys={keys}
      control={form.control}
      register={form.register}
      secretStatus={secretStatus}
      moduleName={mod.moduleName}
    />
  );

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

      <Card className="mb-3">
        <OrkestraCardHeader
          title={t('adminModules.detail.cards.configuration')}
          light={false}
          endEl={
            envLoading ? <Spinner animation="border" size="sm" /> : undefined
          }
        />
        <Card.Body>
          {error && (
            <Alert
              variant="danger"
              className="fs-10"
              dismissible
              onClose={() => setError(null)}
            >
              {error}
            </Alert>
          )}
          {success && (
            <Alert variant="success" className="fs-10">
              {t('adminModules.detail.configCard.saved')}
            </Alert>
          )}

          {/* Bottom padding reserves room above the sticky bar so it never
              overlaps the last field, including at narrow widths where the
              bar's own content wraps onto a second line. */}
          <div className="pb-4">
            {showRail && activeNode ? (
              <Row className="g-3">
                <Col md={4} lg={3}>
                  <ModuleConfigRail
                    tree={groupTree}
                    moduleName={mod.moduleName}
                    activeKey={activeNode.key}
                    onSelect={setActiveKey}
                    statusFor={() => ({ unfilled: 0 })}
                  />
                </Col>
                <Col md={8} lg={9}>
                  <ModuleConfigPanel
                    key={activeNode.key}
                    node={activeNode}
                    moduleName={mod.moduleName}
                    schema={schema}
                    control={form.control}
                    register={form.register}
                    secretStatus={secretStatus}
                  />
                </Col>
              </Row>
            ) : (
              renderFields()
            )}
          </div>

          <ModuleSaveBar
            dirtyCount={dirtyCount}
            perGroup={perGroup}
            errors={saveBarErrors}
            saving={saving}
            onDiscard={handleDiscard}
            onSave={onSave}
          />
        </Card.Body>
      </Card>
    </>
  );
};

export default ModuleConfigSection;
