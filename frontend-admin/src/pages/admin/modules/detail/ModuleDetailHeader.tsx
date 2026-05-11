import { Alert, Form, Spinner } from 'react-bootstrap';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faArrowLeft } from '@fortawesome/free-solid-svg-icons';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { ModuleConfig } from 'store/api/moduleApi';
import { useUpdateModuleMutation } from 'store/api/moduleApi';
import { useState } from 'react';

const statusColors: Record<string, BadgeColor> = {
  running: 'success',
  failed: 'danger',
  disabled: 'secondary',
  stopped: 'warning',
};

const categoryColors: Record<string, BadgeColor> = {
  core: 'primary',
  toggleable: 'info',
  external: 'warning',
};

interface ModuleDetailHeaderProps {
  module: ModuleConfig;
}

const ModuleDetailHeader: React.FC<ModuleDetailHeaderProps> = ({ module: mod }) => {
  const [updateModule] = useUpdateModuleMutation();
  const [toggling, setToggling] = useState(false);

  const isCore = mod.category === 'core';
  const statusLabel = mod.status;

  const handleToggle = async () => {
    if (isCore) return;
    setToggling(true);
    try {
      await updateModule({ name: mod.moduleName, enabled: !mod.enabled }).unwrap();
    } catch {
      // RTK Query handles error state
    } finally {
      setToggling(false);
    }
  };

  return (
    <div className="mb-3">
      <Link to="/admin/modules" className="text-decoration-none fs-10 text-600 mb-2 d-inline-block">
        <FontAwesomeIcon icon={faArrowLeft} className="me-1" />
        Back to Modules
      </Link>

      <div className="d-flex align-items-center justify-content-between flex-wrap gap-2">
        <div>
          <h4 className="mb-1">
            {mod.displayName}
            <SubtleBadge bg={categoryColors[mod.category] || 'secondary'} pill className="ms-2 fs-11">
              {mod.category}
            </SubtleBadge>
            <SubtleBadge bg={statusColors[mod.status] || 'secondary'} pill className="ms-2 fs-11">
              {statusLabel}
            </SubtleBadge>
          </h4>
          <p className="text-muted fs-10 mb-0">{mod.description}</p>
        </div>
        <div className="d-flex align-items-center gap-3">
          {toggling ? (
            <Spinner animation="border" size="sm" />
          ) : (
            <Form.Check
              type="switch"
              id="module-detail-toggle"
              label={mod.enabled ? 'Enabled' : 'Disabled'}
              checked={mod.enabled}
              disabled={isCore}
              onChange={handleToggle}
              className="fs-10"
            />
          )}
        </div>
      </div>

      {mod.status === 'failed' && mod.error && (
        <Alert variant="danger" className="mt-2 py-2 fs-10 mb-0">
          <strong>Init Error:</strong> {mod.error}
        </Alert>
      )}

      {mod.infraContainers && mod.infraContainers.length > 0 && (
        <div className="mt-2 fs-11 text-600">
          <strong className="text-900">Managed infrastructure: </strong>
          {mod.infraContainers.map((c, idx) => {
            const dot = c.error
              ? 'bg-danger'
              : c.running
                ? 'bg-success'
                : 'bg-400';
            return (
              <span key={c.name} className="ms-2">
                {idx > 0 && <span className="me-2">&middot;</span>}
                <span
                  className={`rounded-circle d-inline-block me-1 ${dot}`}
                  style={{ width: 8, height: 8 }}
                />
                <code className="text-700">{c.name}</code>
                <span className="ms-1">
                  {c.error
                    ? `error: ${c.error}`
                    : c.running
                      ? 'running'
                      : 'stopped'}
                </span>
              </span>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default ModuleDetailHeader;
