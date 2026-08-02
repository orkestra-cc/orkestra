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
   * Moves the surrounding rail to another group. Only consumed by the
   * container-node branch below (a declared parent with no fields of its
   * own), which renders its children as the panel's content.
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
 * A declared parent that owns no fields directly — `oauth` sitting over
 * `oauth.google`/`oauth.apple`/… — has nothing to render as a form, and a
 * heading over an empty body reads as a broken page. Such a node instead
 * renders a table of contents of its children, each entry moving the rail
 * there, which is the only thing that panel could usefully offer.
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
  // Only fields currently visible under their own `dependsOn` — a field
  // hidden by its condition must not inflate the count on the toggle, and
  // must not conjure a toggle at all: a group whose only advanced field is
  // currently hidden has nothing "advanced" on screen for the operator to
  // reveal, so the toggle itself is gated on this count, not on
  // `advancedFieldKeys.length` (schema-declared, ignoring visibility).
  const visibleAdvancedCount = visibleFields(schema, values).filter(f =>
    advancedKeys.has(f.key)
  ).length;

  const isContainer = node.fieldKeys.length === 0 && node.children.length > 0;

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
