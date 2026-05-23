import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Card, Form, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faChevronRight } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import ModuleTableHeader from './ModuleTableHeader';
import type { ModuleConfig } from 'store/api/moduleApi';
import {
  useGetModulesQuery,
  useGetModulesHealthQuery,
  useUpdateModuleMutation
} from 'store/api/moduleApi';

const categoryColors: Record<string, BadgeColor> = {
  core: 'primary',
  toggleable: 'info',
  external: 'warning'
};

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

  const addonCategoryOptions = [
    { value: '', label: 'All Categories' },
    { value: 'toggleable', label: 'Toggleable' },
    { value: 'external', label: 'External' }
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

  const getHealthDot = (mod: ModuleConfig): string => {
    const h = healthData?.modules.find(m => m.moduleName === mod.moduleName);
    if (h) return healthDotColors[h.status] || 'bg-400';
    return healthDotColors[mod.status] || 'bg-400';
  };

  const formatDate = (dateStr: string) => {
    if (!dateStr) return '\u2014';
    const date = new Date(dateStr);
    return date.toLocaleDateString('en-GB', {
      day: '2-digit',
      month: 'short',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  if (error) {
    return (
      <Card>
        <Card.Body className="text-center text-danger py-5">
          Failed to load modules. Check your permissions.
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
                      <SubtleBadge
                        bg={categoryColors[mod.category] || 'secondary'}
                        pill
                      >
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
                      <SubtleBadge
                        bg={
                          mod.activeEnvironment === 'production'
                            ? 'success'
                            : mod.activeEnvironment === 'sandbox'
                              ? 'warning'
                              : 'secondary'
                        }
                        pill
                      >
                        {mod.activeEnvironment || 'production'}
                      </SubtleBadge>
                    </td>
                    <td className="text-muted">
                      {mod.dependsOn && mod.dependsOn.length > 0
                        ? mod.dependsOn.join(', ')
                        : '\u2014'}
                    </td>
                    <td className="text-muted">{formatDate(mod.updatedAt)}</td>
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
                          />
                        )}
                        <Link
                          to={`/admin/modules/${mod.moduleName}`}
                          className="text-500 px-1"
                          title={t('adminModules.actions.configure')}
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
                    <td colSpan={6} className="text-center text-muted py-4">
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
            {scopedModules.length} modules total &middot;{' '}
            {scopedModules.filter(m => m.status === 'running').length} running
            &middot; {scopedModules.filter(m => m.status === 'failed').length}{' '}
            failed &middot;{' '}
            {scopedModules.filter(m => m.status === 'disabled').length} disabled
            {scopedModules.filter(m => m.status === 'stopped').length > 0 && (
              <>
                {' '}
                &middot;{' '}
                {scopedModules.filter(m => m.status === 'stopped').length}{' '}
                stopped
              </>
            )}
          </Card.Footer>
        )}
      </Card>
    </>
  );
};

export default ModuleTable;
