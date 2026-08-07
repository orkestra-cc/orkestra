import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Card, Form, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faChevronRight } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import type { BadgeColor } from 'components/common/SubtleBadge';
import ModuleTableHeader from './ModuleTableHeader';
import type { ModuleConfig } from 'store/api/moduleApi';
import {
  useGetModulesQuery,
  useGetModulesHealthQuery,
  useUpdateModuleMutation
} from 'store/api/moduleApi';

// Status colors mean status (DESIGN.md): only the runtime-status chip wears
// a hue. Category (core/toggleable/external) and environment are identity,
// not state — they render neutral, so a `production` chip can never be
// mistaken for the green `running` one at scan distance.
const statusColors: Record<string, BadgeColor> = {
  running: 'success',
  failed: 'danger',
  disabled: 'secondary',
  stopped: 'warning'
};

type ModuleScope = 'core' | 'addons';

interface ModuleTableProps {
  scope?: ModuleScope;
  title?: string;
}

const healthDotColors: Record<string, string> = {
  running: 'bg-success',
  healthy: 'bg-success',
  failed: 'bg-danger',
  unhealthy: 'bg-danger',
  disabled: 'bg-400',
  stopped: 'bg-warning'
};

const ModuleTable: React.FC<ModuleTableProps> = ({ scope, title }) => {
  const { t } = useTranslation();
  const { data: modules, isLoading, error } = useGetModulesQuery();
  const { data: healthData } = useGetModulesHealthQuery();
  const [updateModule] = useUpdateModuleMutation();

  const [searchTerm, setSearchTerm] = useState('');
  const [categoryFilter, setCategoryFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [togglingModule, setTogglingModule] = useState<string | null>(null);

  const scopedModules = useMemo(() => {
    if (!modules) return [];
    if (scope === 'core') return modules.filter(m => m.category === 'core');
    if (scope === 'addons') return modules.filter(m => m.category !== 'core');
    return modules;
  }, [modules, scope]);

  const filteredModules = useMemo(() => {
    return scopedModules.filter(m => {
      if (
        searchTerm &&
        !m.displayName.toLowerCase().includes(searchTerm.toLowerCase()) &&
        !m.moduleName.toLowerCase().includes(searchTerm.toLowerCase())
      ) {
        return false;
      }
      if (categoryFilter && m.category !== categoryFilter) return false;
      if (statusFilter && m.status !== statusFilter) return false;
      return true;
    });
  }, [scopedModules, searchTerm, categoryFilter, statusFilter]);

  // These shadow `ModuleTableHeader`'s own defaults, which are already built
  // from `t()`. Spelled in English here, they made the Core tab render its
  // filter in the active locale and the Addons tab render it in English —
  // side by side, in the same control.
  const addonCategoryOptions = [
    { value: '', label: t('adminModules.filters.allCategories') },
    {
      value: 'toggleable',
      label: t('adminModules.filters.categoryToggleable')
    },
    { value: 'external', label: t('adminModules.filters.categoryExternal') }
  ];

  const handleToggle = async (mod: ModuleConfig) => {
    if (mod.category === 'core') return;
    setTogglingModule(mod.moduleName);
    try {
      await updateModule({
        name: mod.moduleName,
        enabled: !mod.enabled
      }).unwrap();
    } catch {
      // RTK Query handles error state
    } finally {
      setTogglingModule(null);
    }
  };

  // Built from `t()` rather than spelled inline: this whole line used to be
  // hardcoded English sitting under a table whose headers were translated.
  // `stopped` is appended only when non-zero, as before.
  const countBy = (status: string) =>
    scopedModules.filter(m => m.status === status).length;
  const footerSummary = [
    t('adminModules.footer.total', { count: scopedModules.length }),
    t('adminModules.footer.running', { count: countBy('running') }),
    t('adminModules.footer.failed', { count: countBy('failed') }),
    t('adminModules.footer.disabled', { count: countBy('disabled') }),
    ...(countBy('stopped') > 0
      ? [t('adminModules.footer.stopped', { count: countBy('stopped') })]
      : [])
  ].join(' · ');

  const getHealthDot = (mod: ModuleConfig): string => {
    const h = healthData?.modules.find(m => m.moduleName === mod.moduleName);
    if (h) return healthDotColors[h.status] || 'bg-400';
    return healthDotColors[mod.status] || 'bg-400';
  };

  if (error) {
    return (
      <Card>
        <Card.Body className="text-center text-danger py-5">
          {t('adminModules.loadError')}
        </Card.Body>
      </Card>
    );
  }

  return (
    <>
      <Card>
        <Card.Header className="border-bottom border-200 px-4 py-3">
          <ModuleTableHeader
            title={title}
            searchTerm={searchTerm}
            onSearchChange={setSearchTerm}
            categoryFilter={categoryFilter}
            onCategoryChange={setCategoryFilter}
            categoryOptions={
              scope === 'addons' ? addonCategoryOptions : undefined
            }
            hideCategoryFilter={scope === 'core'}
            statusFilter={statusFilter}
            onStatusChange={setStatusFilter}
          />
        </Card.Header>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="text-center py-5">
              <Spinner animation="border" size="sm" />
            </div>
          ) : (
            <Table responsive size="sm" className="fs-10 mb-0 overflow-hidden">
              <thead className="bg-body-tertiary">
                <tr>
                  <th className="pe-4 ps-3">
                    {t('adminModules.columns.module')}
                  </th>
                  <th>{t('adminModules.columns.category')}</th>
                  <th>{t('adminModules.columns.status')}</th>
                  <th>{t('adminModules.columns.environment')}</th>
                  <th>{t('adminModules.columns.dependencies')}</th>
                  <th>{t('adminModules.columns.updated')}</th>
                  <th className="text-end pe-4">
                    {t('adminModules.columns.actions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {filteredModules.map(mod => (
                  <tr key={mod.moduleName} className="align-middle">
                    <td className="ps-3">
                      <div className="d-flex align-items-center gap-2">
                        <span
                          className={`rounded-circle ${getHealthDot(mod)}`}
                          style={{ width: 8, height: 8, flexShrink: 0 }}
                        />
                        <div>
                          <Link
                            to={`/admin/modules/${mod.moduleName}`}
                            className="fw-semibold text-900 text-decoration-none"
                          >
                            {mod.displayName}
                          </Link>
                          <div className="text-700 fs-11">
                            {mod.description}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td>
                      <SubtleBadge bg="secondary" pill>
                        {mod.category}
                      </SubtleBadge>
                    </td>
                    <td>
                      <SubtleBadge
                        bg={statusColors[mod.status] || 'secondary'}
                        pill
                      >
                        {mod.status}
                      </SubtleBadge>
                      {mod.error && (
                        <div
                          className="text-danger fs-11 mt-1"
                          title={mod.error}
                        >
                          {mod.error.length > 60
                            ? mod.error.slice(0, 60) + '...'
                            : mod.error}
                        </div>
                      )}
                    </td>
                    <td>
                      <SubtleBadge bg="secondary" pill>
                        {mod.activeEnvironment || 'production'}
                      </SubtleBadge>
                    </td>
                    <td className="text-muted">
                      {mod.dependsOn && mod.dependsOn.length > 0
                        ? mod.dependsOn.join(', ')
                        : '\u2014'}
                    </td>
                    <td className="text-muted">
                      {formatDateTime(mod.updatedAt)}
                    </td>
                    <td className="text-end pe-4">
                      <div className="d-flex align-items-center justify-content-end gap-2">
                        {togglingModule === mod.moduleName ? (
                          <Spinner animation="border" size="sm" />
                        ) : (
                          <Form.Check
                            type="switch"
                            checked={mod.enabled}
                            disabled={mod.category === 'core'}
                            onChange={() => handleToggle(mod)}
                            title={
                              mod.category === 'core'
                                ? t('adminModules.toggleTitles.coreLocked')
                                : mod.enabled
                                  ? t('adminModules.toggleTitles.disable')
                                  : t('adminModules.toggleTitles.enable')
                            }
                            // The switch is the row's highest-stakes control:
                            // a screen reader must hear WHICH module it is
                            // about to start or stop, not a bare "switch".
                            aria-label={`${
                              mod.category === 'core'
                                ? t('adminModules.toggleTitles.coreLocked')
                                : mod.enabled
                                  ? t('adminModules.toggleTitles.disable')
                                  : t('adminModules.toggleTitles.enable')
                            } — ${mod.displayName}`}
                          />
                        )}
                        <Link
                          to={`/admin/modules/${mod.moduleName}`}
                          className="text-600 px-1"
                          title={t('adminModules.actions.configure')}
                          aria-label={`${t('adminModules.actions.configure')} ${mod.displayName}`}
                        >
                          <FontAwesomeIcon
                            icon={faChevronRight}
                            className="fs-10"
                          />
                        </Link>
                      </div>
                    </td>
                  </tr>
                ))}
                {filteredModules.length === 0 && (
                  <tr>
                    {/* 7, not 6 — the row has seven columns, and a short
                        colSpan leaves the empty state announced against a
                        malformed row. */}
                    <td colSpan={7} className="text-center text-muted py-4">
                      {t('adminModules.noMatch')}
                    </td>
                  </tr>
                )}
              </tbody>
            </Table>
          )}
        </Card.Body>
        {modules && (
          <Card.Footer className="fs-10 text-muted">
            {footerSummary}
          </Card.Footer>
        )}
      </Card>
    </>
  );
};

export default ModuleTable;
