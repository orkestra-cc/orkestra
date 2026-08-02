import { useState } from 'react';
import { Button, Collapse } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { ConfigField } from 'store/api/moduleApi';
import type { GroupNode } from '../configModel';
import { visibleFields } from '../configModel';
import { translateConfigGroup } from 'helpers/configLabel';
import ModuleConfigFields from '../ModuleConfigFields';
import type { ModuleConfigFieldsProps } from '../ModuleConfigFields';

/** The subset of `ModuleConfigFieldsProps` the panel doesn't compute itself. */
export type ModuleConfigPanelFieldProps = Omit<
  ModuleConfigFieldsProps,
  'schema' | 'moduleName' | 'includeKeys'
>;

export interface ModuleConfigPanelProps extends ModuleConfigPanelFieldProps {
  node: GroupNode;
  moduleName: string;
  schema: ConfigField[];
}

/**
 * Renders one group node: translated heading + description, then that
 * node's own fields (`node.fieldKeys` — never the whole schema, so a nested
 * group's fields don't leak into its parent's panel or vice versa).
 *
 * Fields carrying `advanced: true` are pulled out of the main list and
 * rendered inside a collapsed section instead, so a rarely-touched setting
 * doesn't compete for attention with the fields an operator actually needs.
 */
const ModuleConfigPanel: React.FC<ModuleConfigPanelProps> = ({
  node,
  moduleName,
  schema,
  configValues,
  ...fieldProps
}) => {
  const { t } = useTranslation();
  const [showAdvanced, setShowAdvanced] = useState(false);

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
  // hidden by its condition must not inflate the badge on the toggle.
  const visibleAdvancedCount = visibleFields(schema, configValues).filter(f =>
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
        configValues={configValues}
        {...fieldProps}
      />
      {advancedFieldKeys.length > 0 && (
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
                configValues={configValues}
                {...fieldProps}
              />
            </div>
          </Collapse>
        </>
      )}
    </div>
  );
};

export default ModuleConfigPanel;
