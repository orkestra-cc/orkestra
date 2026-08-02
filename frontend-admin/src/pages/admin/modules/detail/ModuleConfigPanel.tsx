import { useState } from 'react';
import { Button, Collapse } from 'react-bootstrap';
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
 */
const ModuleConfigPanel: React.FC<ModuleConfigPanelProps> = ({
  node,
  moduleName,
  schema,
  control,
  register,
  secretStatus
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
