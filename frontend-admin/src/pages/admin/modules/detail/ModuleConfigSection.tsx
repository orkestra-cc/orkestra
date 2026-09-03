import { useState } from 'react';
import { Alert, Card, Col, Row, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { OrkestraCardHeader } from 'components/common';
import type { ModuleConfig } from 'store/api/moduleApi';
import { hasCardRail } from '../configModel';
import type { ModuleConfigController } from '../useModuleConfigController';
import ModuleConfigFields from '../ModuleConfigFields';
import ModuleConfigRail from './ModuleConfigRail';
import ModuleConfigPanel from './ModuleConfigPanel';
import ModuleSaveBar from './ModuleSaveBar';

interface ModuleConfigSectionProps {
  module: ModuleConfig;
  /**
   * The module's shared config controller (`useModuleConfigController`),
   * owned and instantiated exactly once by `detail/index.tsx` — this
   * component never creates its own. Sharing one instance is what lets
   * `detail/index.tsx` register a single `useBlocker` that always reads the
   * real dirty state, however this card renders it.
   */
  controller: ModuleConfigController;
}

/**
 * The config-card-only layout: rail + panel (or the flat form) inside one
 * Card, used whenever the module doesn't declare enough groups for the
 * full-page rail (`hasPageRail` in `configModel.ts`) — see `detail/index.tsx`
 * for that decision. `activeKey` stays local state here: this card's own
 * rail is independent of the page's `?section=`, which only exists once the
 * full-page rail takes over.
 */
const ModuleConfigSection: React.FC<ModuleConfigSectionProps> = ({
  module: mod,
  controller
}) => {
  const { t } = useTranslation();
  const [activeKey, setActiveKey] = useState('');

  const {
    schema,
    groupTree,
    flatNodes,
    form,
    fieldNames,
    secretStatus,
    envLoading,
    saving,
    dirtyCount,
    errorCount,
    perGroup,
    saveBarErrors,
    unfilledByGroup,
    error,
    success,
    conflict,
    reloadAndReview,
    clearError,
    onSave,
    handleDiscard
  } = controller;

  const showRail = hasCardRail(groupTree, mod.configGroups);
  const currentKey = activeKey || flatNodes[0]?.key || '';
  // A key can survive in state past a structural group change (module
  // config changed shape) and no longer match anything in the current tree.
  // Falling back to the first node keeps the rail and panel in sync instead
  // of silently dropping to the unscoped flat form below.
  const activeNode =
    flatNodes.find(node => node.key === currentKey) ?? flatNodes[0];

  // Both breakdowns are gated on `showRail`: with a single implicit bucket
  // (the flat/legacy form), a per-group chip only restates the aggregate
  // count already shown, and a "Go to <group>" button has nowhere useful to
  // navigate — the field it points at is already the only thing on screen.
  const perGroupDisplay = showRail ? perGroup : [];
  const saveBarErrorsDisplay = showRail
    ? saveBarErrors.map(g => ({ ...g, onSelect: () => setActiveKey(g.key) }))
    : [];

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
      fieldNames={fieldNames}
      secretStatus={secretStatus}
      moduleName={mod.moduleName}
    />
  );

  return (
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
            // Not dismissible while the conflict is latched: Save is disabled
            // and this banner is the only thing saying why. Dismissed, the
            // operator is left with a greyed Save and a yellow button, and the
            // plausible next move is Discard — which destroys the very draft
            // (typed secret included) the latch exists to protect.
            dismissible={!conflict}
            onClose={clearError}
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
                  statusFor={node => ({
                    unfilled: unfilledByGroup.get(node.key) ?? 0
                  })}
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
                  fieldNames={fieldNames}
                  secretStatus={secretStatus}
                  onSelectGroup={setActiveKey}
                />
              </Col>
            </Row>
          ) : (
            renderFields()
          )}
        </div>

        <ModuleSaveBar
          dirtyCount={dirtyCount}
          perGroup={perGroupDisplay}
          errorCount={errorCount}
          errors={saveBarErrorsDisplay}
          saving={saving}
          conflict={conflict}
          onReload={reloadAndReview}
          onDiscard={handleDiscard}
          onSave={onSave}
        />
      </Card.Body>
    </Card>
  );
};

export default ModuleConfigSection;
