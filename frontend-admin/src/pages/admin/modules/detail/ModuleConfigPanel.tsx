import { useState } from 'react';
import { Button, Collapse, ListGroup } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useWatch, type Control, type UseFormRegister } from 'react-hook-form';
import type { ConfigField } from 'store/api/moduleApi';
import type { GroupNode } from '../configModel';
import { visibleFields } from '../configModel';
import { translateConfigGroup } from 'helpers/configLabel';
import ModuleConfigFields from '../ModuleConfigFields';
import type { ConfigFormValues } from '../useModuleConfigForm';

export interface ModuleConfigPanelProps {
  node: GroupNode;
  moduleName: string;
  schema: ConfigField[];
  control: Control<ConfigFormValues>;
  register: UseFormRegister<ConfigFormValues>;
  secretStatus?: Record<string, boolean>;
  /**
   * Moves the surrounding rail to another group. Consumed by the
   * container-node branch below (a declared parent with no fields of its
   * own), which renders its children as the panel's content, and by the
   * all-hidden-leaf empty state, which offers a way back to the parent
   * group where the gating options live.
   */
  onSelectGroup?: (key: string) => void;
}

/**
 * Renders one group node: translated heading + description, then that
 * node's own fields (`node.fieldKeys` — never the whole schema, so a nested
 * group's fields don't leak into its parent's panel or vice versa). All
 * panels share the same `control`/`register` from the module-wide
 * react-hook-form instance, so switching the active node never remounts the
 * form — only which slice of it is on screen.
 *
 * Fields carrying `advanced: true` are pulled out of the main list and
 * rendered inside a collapsed section instead, so a rarely-touched setting
 * doesn't compete for attention with the fields an operator actually needs.
 *
 * A declared parent that owns no fields of its own — a group that exists
 * purely to nest children — has nothing to render as a form, and a heading
 * over an empty body reads as a broken page. Such a node instead renders a
 * table of contents of its children, each entry moving the rail there, which
 * is the only thing that panel could usefully offer. No module in the base
 * declares a group of that shape today (`auth`'s `oauth` nests the four
 * providers but also owns 11 fields of its own, so it takes the normal form
 * branch); the container branch exists for addons that do, and is covered by
 * fixtures.
 *
 * A *leaf* node can land in the same "heading over an empty body" state for a
 * different reason: it owns fields, but every one of them is currently
 * hidden by an unmet `dependsOn` (phase 4's `oauth.google` before either of
 * Google's two enable toggles is on). It has no children to link to, so the
 * container branch above cannot fire — it needs its own honest empty state
 * instead of silently rendering nothing, and a way back to wherever the
 * options it waits on actually live (its parent group).
 */
const ModuleConfigPanel: React.FC<ModuleConfigPanelProps> = ({
  node,
  moduleName,
  schema,
  control,
  register,
  secretStatus,
  onSelectGroup
}) => {
  const { t } = useTranslation();
  const [showAdvanced, setShowAdvanced] = useState(false);
  const values = useWatch({ control }) as ConfigFormValues;

  const label = translateConfigGroup(t, moduleName, node);
  const description = node.description
    ? translateConfigGroup(t, moduleName, node, 'desc')
    : '';

  const advancedKeys = new Set(
    schema
      .filter(f => f.advanced && node.fieldKeys.includes(f.key))
      .map(f => f.key)
  );
  const mainKeys = node.fieldKeys.filter(key => !advancedKeys.has(key));
  const advancedFieldKeys = node.fieldKeys.filter(key => advancedKeys.has(key));
  // Fields of this node currently visible under their own `dependsOn` — a
  // field hidden by its condition must not inflate the Advanced toggle's
  // count, must not conjure the toggle at all (a group whose only advanced
  // field is hidden has nothing "advanced" on screen to reveal), and must
  // not count toward the node having anything to show at all (see
  // `hasVisibleFields` below).
  const visibleNodeFields = visibleFields(schema, values).filter(f =>
    node.fieldKeys.includes(f.key)
  );
  const visibleAdvancedCount = visibleNodeFields.filter(f =>
    advancedKeys.has(f.key)
  ).length;

  const isContainer = node.fieldKeys.length === 0 && node.children.length > 0;
  // A leaf node (no children) can still own zero *visible* fields — every
  // one of them declared but currently hidden by an unmet `dependsOn`. That
  // is a real, expected state (phase 4's `oauth.google` before either of
  // Google's two enable toggles is on) — not a bug — but rendering the main
  // list below would silently produce nothing under the heading, which is
  // the same "broken page" the container branch above exists to avoid.
  const hasVisibleFields = visibleNodeFields.length > 0;

  if (isContainer) {
    return (
      <div>
        <h5 className="fs-9 fw-semibold mb-1">{label}</h5>
        {description && <p className="text-muted fs-10 mb-3">{description}</p>}
        <ListGroup variant="flush">
          {node.children.map(child => {
            const childDesc = child.description
              ? translateConfigGroup(t, moduleName, child, 'desc')
              : '';
            return (
              <ListGroup.Item
                key={child.key}
                action
                onClick={() => onSelectGroup?.(child.key)}
                className="px-0"
              >
                <span className="fs-10 fw-semibold">
                  {translateConfigGroup(t, moduleName, child)}
                </span>
                {childDesc && (
                  <span className="d-block text-muted fs-11">{childDesc}</span>
                )}
              </ListGroup.Item>
            );
          })}
        </ListGroup>
      </div>
    );
  }

  if (!hasVisibleFields) {
    // "These settings appear once the options they depend on are enabled" is
    // a dead end on its own — it never says *where* those options are. The
    // gating toggles are almost always declared one level up (a provider's
    // enable switches sit on the parent group, its credentials on the leaf),
    // so when this node has a parent, name it and offer a way there. Stays
    // generic: any addon can produce this shape, and a node with no parent
    // still gets the original, unqualified sentence.
    const parent = node.parent;
    const parentLabel = parent
      ? translateConfigGroup(t, moduleName, parent)
      : '';
    return (
      <div>
        <h5 className="fs-9 fw-semibold mb-1">{label}</h5>
        {description && <p className="text-muted fs-10 mb-3">{description}</p>}
        <p className="text-muted fs-10 mb-0">
          {parent
            ? t('adminModules.detail.rail.emptyUntilDependencyIn', {
                group: parentLabel
              })
            : t('adminModules.detail.rail.emptyUntilDependency')}
        </p>
        {parent && onSelectGroup && (
          <Button
            variant="link"
            size="sm"
            className="ps-0 text-decoration-none"
            onClick={() => onSelectGroup(parent.key)}
          >
            {t('adminModules.detail.rail.goToParent', { group: parentLabel })}
          </Button>
        )}
      </div>
    );
  }

  return (
    <div>
      <h5 className="fs-9 fw-semibold mb-1">{label}</h5>
      {description && <p className="text-muted fs-10 mb-3">{description}</p>}
      <ModuleConfigFields
        schema={schema}
        moduleName={moduleName}
        includeKeys={mainKeys}
        control={control}
        register={register}
        secretStatus={secretStatus}
      />
      {visibleAdvancedCount > 0 && (
        <>
          <Button
            variant="link"
            size="sm"
            className="ps-0 text-decoration-none"
            aria-expanded={showAdvanced}
            onClick={() => setShowAdvanced(prev => !prev)}
          >
            {t('adminModules.detail.rail.advancedToggle', {
              count: visibleAdvancedCount
            })}
          </Button>
          <Collapse in={showAdvanced} unmountOnExit>
            <div>
              <ModuleConfigFields
                schema={schema}
                moduleName={moduleName}
                includeKeys={advancedFieldKeys}
                control={control}
                register={register}
                secretStatus={secretStatus}
              />
            </div>
          </Collapse>
        </>
      )}
    </div>
  );
};

export default ModuleConfigPanel;
