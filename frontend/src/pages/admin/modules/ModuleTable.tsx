import { useMemo, useState } from 'react';
import { Button, Card, Form, Spinner, Table } from 'react-bootstrap';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import ModuleTableHeader from './ModuleTableHeader';
import ModuleConfigModal from './ModuleConfigModal';
import type { ModuleConfig } from 'store/api/moduleApi';
import {
  useGetModulesQuery,
  useUpdateModuleMutation,
} from 'store/api/moduleApi';

const categoryColors: Record<string, BadgeColor> = {
  core: 'primary',
  toggleable: 'info',
  external: 'warning',
};

const statusColors: Record<string, BadgeColor> = {
  running: 'success',
  failed: 'danger',
  disabled: 'secondary',
};

type ModuleScope = 'core' | 'addons';

interface ModuleTableProps {
  scope?: ModuleScope;
  title?: string;
}

const ModuleTable: React.FC<ModuleTableProps> = ({ scope, title }) => {
  const { data: modules, isLoading, error } = useGetModulesQuery();
  const [updateModule] = useUpdateModuleMutation();

  const [searchTerm, setSearchTerm] = useState('');
  const [categoryFilter, setCategoryFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [selectedModule, setSelectedModule] = useState<ModuleConfig | null>(
    null
  );
  const [showConfigModal, setShowConfigModal] = useState(false);
  const [togglingModule, setTogglingModule] = useState<string | null>(null);

  const scopedModules = useMemo(() => {
    if (!modules) return [];
    if (scope === 'core') return modules.filter((m) => m.category === 'core');
    if (scope === 'addons')
      return modules.filter((m) => m.category !== 'core');
    return modules;
  }, [modules, scope]);

  const filteredModules = useMemo(() => {
    return scopedModules.filter((m) => {
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
    { value: 'external', label: 'External' },
  ];

  const handleToggle = async (mod: ModuleConfig) => {
    if (mod.category === 'core') return;
    setTogglingModule(mod.moduleName);
    try {
      await updateModule({
        name: mod.moduleName,
        enabled: !mod.enabled,
      }).unwrap();
    } catch {
      // RTK Query handles error state
    } finally {
      setTogglingModule(null);
    }
  };

  const handleConfigure = (mod: ModuleConfig) => {
    setSelectedModule(mod);
    setShowConfigModal(true);
  };

  const formatDate = (dateStr: string) => {
    if (!dateStr) return '\u2014';
    const date = new Date(dateStr);
    return date.toLocaleDateString('en-GB', {
      day: '2-digit',
      month: 'short',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
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
            <Table
              responsive
              size="sm"
              className="fs-10 mb-0 overflow-hidden"
            >
              <thead className="bg-body-tertiary">
                <tr>
                  <th className="pe-4 ps-3">Module</th>
                  <th>Category</th>
                  <th>Status</th>
                  <th>Dependencies</th>
                  <th>Updated</th>
                  <th className="text-end pe-4">Actions</th>
                </tr>
              </thead>
              <tbody>
                {filteredModules.map((mod) => (
                  <tr key={mod.moduleName} className="align-middle">
                    <td className="ps-3">
                      <div className="fw-semibold">{mod.displayName}</div>
                      <div className="text-muted fs-11">
                        {mod.description}
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
                                ? 'Core modules cannot be disabled'
                                : mod.enabled
                                  ? 'Disable module'
                                  : 'Enable module'
                            }
                          />
                        )}
                        <Button
                          variant="link"
                          size="sm"
                          className="p-0 text-decoration-none"
                          onClick={() => handleConfigure(mod)}
                        >
                          Configure
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
                {filteredModules.length === 0 && (
                  <tr>
                    <td colSpan={6} className="text-center text-muted py-4">
                      No modules match the current filters.
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
            {scopedModules.filter((m) => m.status === 'running').length} running
            &middot;{' '}
            {scopedModules.filter((m) => m.status === 'failed').length} failed
            &middot;{' '}
            {scopedModules.filter((m) => m.status === 'disabled').length}{' '}
            disabled
          </Card.Footer>
        )}
      </Card>

      <ModuleConfigModal
        module={selectedModule}
        show={showConfigModal}
        onHide={() => setShowConfigModal(false)}
      />
    </>
  );
};

export default ModuleTable;
