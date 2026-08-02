import { forwardRef, useCallback, useEffect, useMemo, useState } from 'react';
import { useBlocker } from 'react-router';
import {
  Alert,
  Anchor,
  Button,
  Card,
  Modal,
  Nav,
  Spinner
} from 'react-bootstrap';
import type { AnchorProps } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { OrkestraCardHeader } from 'components/common';
import type {
  ModuleConfig,
  EnvironmentConfigResponse
} from 'store/api/moduleApi';
import {
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation
} from 'store/api/moduleApi';
import ModuleConfigFields from '../ModuleConfigFields';
import { buildGroupTree, isFieldVisible } from '../configModel';
import { translateConfigGroup } from 'helpers/configLabel';

interface ModuleConfigSectionProps {
  module: ModuleConfig;
  selectedEnvironment: string;
}

/**
 * Once the tab `<Nav>` below declares `role="tablist"`, `@restart/ui` applies
 * a roving tabIndex — every inactive `Nav.Link` gets `tabIndex={-1}` — which
 * is only correct paired with the ArrowLeft/ArrowRight handler `Nav.js` wires
 * up when it detects a `<Tab.Container>`. This is a bare `<Nav>` (phase 3
 * replaces it with a vertical rail, so adopting `Tab.Container` here would be
 * structural investment thrown away for a "no visual change" task), so
 * without a fix every non-active tab becomes unreachable by keyboard or
 * screen reader: no sequential Tab (tabIndex -1) and no arrow keys (the
 * handler bails out before it runs).
 *
 * Passing `tabIndex={0}` straight on `<Nav.Link>` does **not** work —
 * react-bootstrap's `NavLink` spreads `{...props, ...navItemProps}`, and
 * `navItemProps` (computed *after* ours, with the -1) always wins. Routing
 * through `Nav.Link`'s own default `as` (`Anchor`) ourselves, with an
 * explicit `tabIndex` applied after the incoming props, restores exactly the
 * sequential reachability every tab had before `role="tablist"` was added
 * (previously via `role="button"`'s default `tabIndex={0}`).
 */
const ReachableTabAnchor = forwardRef<HTMLAnchorElement, AnchorProps>(
  (props, ref) => <Anchor ref={ref} {...props} tabIndex={0} />
);
ReachableTabAnchor.displayName = 'ReachableTabAnchor';

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

  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [secretValues, setSecretValues] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [activeTab, setActiveTab] = useState('');

  // Track the initial loaded values for dirty detection.
  const [loadedValues, setLoadedValues] = useState<Record<string, string>>({});

  // Reset form when environment data loads or changes.
  const resetForm = useCallback(
    (data: EnvironmentConfigResponse | undefined) => {
      if (data) {
        setConfigValues({ ...(data.configValues ?? {}) });
        setLoadedValues({ ...(data.configValues ?? {}) });
      } else {
        setConfigValues({ ...(mod.configValues ?? {}) });
        setLoadedValues({ ...(mod.configValues ?? {}) });
      }
      setSecretValues({});
      setError(null);
      setSuccess(false);
    },
    [mod.configValues]
  );

  useEffect(() => {
    resetForm(envConfig);
  }, [envConfig, resetForm]);

  const schema = mod.configSchema ?? [];
  const groupTree = useMemo(
    () => buildGroupTree(schema, mod.configGroups),
    [schema, mod.configGroups]
  );
  // Legacy heuristic (no declared groups): only show tabs once there are ≥2
  // synthesized buckets — a single bucket would be a redundant one-tab rail.
  // That threshold matched `bucketByGroup`'s caller and must keep matching
  // it exactly (no module declares groups today, so this is the only branch
  // any current module hits). A module that *does* declare configGroups
  // opted into an explicit rail, so it always gets tabs, even a lone one —
  // its single top-level node can still have nested children (rendered
  // starting phase 3), which the legacy flat-bucket case never has.
  const showTabs = groupTree.length >= 2 || Boolean(mod.configGroups?.length);
  const currentTab = activeTab || groupTree[0]?.key || '';

  const secretStatus = envConfig?.secretStatus ?? mod.secretStatus ?? {};

  // Dirty detection
  const isDirty = useMemo(() => {
    const hasSecrets = Object.values(secretValues).some(v => v.trim() !== '');
    if (hasSecrets) return true;
    for (const field of schema) {
      if (field.type === 'secret') continue;
      if (!isFieldVisible(field, configValues, schema)) continue;
      if ((configValues[field.key] || '') !== (loadedValues[field.key] || ''))
        return true;
    }
    return false;
  }, [configValues, loadedValues, secretValues, schema]);

  const handleSave = async () => {
    setError(null);
    setSuccess(false);

    try {
      const changedConfig: Record<string, string> = {};
      for (const field of schema) {
        if (field.type === 'secret') continue;
        if (!isFieldVisible(field, configValues, schema)) continue;
        if (
          (configValues[field.key] || '') !== (loadedValues[field.key] || '')
        ) {
          changedConfig[field.key] = configValues[field.key] || '';
        }
      }

      const newSecrets: Record<string, string> = {};
      for (const [key, value] of Object.entries(secretValues)) {
        if (value.trim()) newSecrets[key] = value;
      }

      if (
        Object.keys(changedConfig).length === 0 &&
        Object.keys(newSecrets).length === 0
      ) {
        return;
      }

      await updateEnv({
        name: mod.moduleName,
        environment: selectedEnvironment,
        config:
          Object.keys(changedConfig).length > 0 ? changedConfig : undefined,
        secrets: Object.keys(newSecrets).length > 0 ? newSecrets : undefined
      }).unwrap();

      setSuccess(true);
      setSecretValues({});
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
  };

  const handleDiscard = () => {
    resetForm(envConfig);
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
      configValues={configValues}
      secretValues={secretValues}
      secretStatus={secretStatus}
      moduleName={mod.moduleName}
      onConfigChange={(key, value) =>
        setConfigValues(prev => ({ ...prev, [key]: value }))
      }
      onSecretChange={(key, value) =>
        setSecretValues(prev => ({ ...prev, [key]: value }))
      }
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

          {showTabs ? (
            <>
              <Nav
                variant="tabs"
                role="tablist"
                activeKey={currentTab}
                onSelect={k => setActiveTab(k || '')}
                className="mb-3"
              >
                {groupTree.map(node => (
                  <Nav.Item key={node.key}>
                    <Nav.Link eventKey={node.key} as={ReachableTabAnchor}>
                      {translateConfigGroup(t, mod.moduleName, node)}
                    </Nav.Link>
                  </Nav.Item>
                ))}
              </Nav>
              {groupTree.map(node =>
                currentTab === node.key ? (
                  <div key={node.key}>{renderFields(node.fieldKeys)}</div>
                ) : null
              )}
            </>
          ) : (
            renderFields()
          )}

          <div className="d-flex justify-content-end gap-2 mt-3 pt-3 border-top">
            {isDirty && (
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={handleDiscard}
              >
                {t('adminModules.detail.configCard.discard')}
              </Button>
            )}
            <Button
              variant="primary"
              size="sm"
              onClick={handleSave}
              disabled={saving || !isDirty}
            >
              {saving ? (
                <Spinner animation="border" size="sm" />
              ) : (
                t('adminModules.detail.configCard.saveChanges')
              )}
            </Button>
          </div>
        </Card.Body>
      </Card>
    </>
  );
};

export default ModuleConfigSection;
