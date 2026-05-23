import { useMemo } from 'react';
import { Alert, Badge, Button, Card, Form, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { useTranslation } from 'react-i18next';
import {
  useGetLogLevelsQuery,
  useSetGlobalLogLevelMutation,
  useSetModuleLogLevelMutation,
  useUnsetModuleLogLevelMutation,
  useResetLogLevelsMutation
} from 'store/api/observabilityApi';
import { LOG_LEVELS, type LogLevel } from 'types/observability';

// LogLevelsPage — ADR-0005 Phase F admin surface for runtime
// log-level mutation. Two interactions:
//
//   1. Global dropdown sets the default threshold every module
//      inherits unless it has an explicit override.
//   2. Per-row dropdown sets a per-module override; the "Revert"
//      link removes the override and the row falls back to Global.
//
// Mutations return the fresh LogLevelsView so the table re-renders
// without an extra refetch — the backend View() is in-memory cheap.

const levelVariant: Record<LogLevel, string> = {
  debug: 'secondary',
  info: 'primary',
  warn: 'warning',
  error: 'danger'
};

const LogLevelsPage: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetLogLevelsQuery();
  const [setGlobal, setGlobalStatus] = useSetGlobalLogLevelMutation();
  const [setModule] = useSetModuleLogLevelMutation();
  const [unsetModule] = useUnsetModuleLogLevelMutation();
  const [resetAll, resetStatus] = useResetLogLevelsMutation();

  const lastUpdated = useMemo(() => {
    if (!data?.updatedAt) return null;
    try {
      return new Date(data.updatedAt).toLocaleString();
    } catch {
      return data.updatedAt;
    }
  }, [data?.updatedAt]);

  const handleGlobal = async (level: LogLevel) => {
    try {
      await setGlobal({ level }).unwrap();
      toast.success(
        t('adminObservability.logLevels.globalSetToast', { level })
      );
    } catch {
      toast.error(t('adminObservability.logLevels.globalFailToast'));
    }
  };

  const handleModule = async (moduleName: string, level: LogLevel) => {
    try {
      await setModule({ module: moduleName, level }).unwrap();
      toast.success(
        t('adminObservability.logLevels.moduleSetToast', {
          module: moduleName,
          level
        })
      );
    } catch {
      toast.error(
        t('adminObservability.logLevels.moduleFailToast', {
          module: moduleName
        })
      );
    }
  };

  const handleRevert = async (moduleName: string) => {
    try {
      await unsetModule({ module: moduleName }).unwrap();
      toast.success(
        t('adminObservability.logLevels.moduleRevertToast', {
          module: moduleName
        })
      );
    } catch {
      toast.error(
        t('adminObservability.logLevels.revertFailToast', {
          module: moduleName
        })
      );
    }
  };

  const handleResetAll = async () => {
    if (!window.confirm(t('adminObservability.logLevels.confirmReset'))) {
      return;
    }
    try {
      await resetAll().unwrap();
      toast.success(t('adminObservability.logLevels.resetDoneToast'));
    } catch {
      toast.error(t('adminObservability.logLevels.resetFailToast'));
    }
  };

  if (isLoading) {
    return (
      <Card>
        <Card.Body>{t('adminObservability.logLevels.loading')}</Card.Body>
      </Card>
    );
  }

  if (error || !data) {
    return (
      <Alert variant="danger">
        {t('adminObservability.logLevels.loadFailed')}
      </Alert>
    );
  }

  return (
    <>
      <Card className="shadow-none border mb-3">
        <Card.Body className="d-flex align-items-center justify-content-between gap-3 flex-wrap">
          <div>
            <h5 className="mb-1">
              <FontAwesomeIcon icon="sliders-h" className="me-2 text-primary" />
              {t('adminObservability.logLevels.title')}
            </h5>
            <p className="text-muted mb-0 small">
              {t('adminObservability.logLevels.description')}
            </p>
            {lastUpdated && (
              <p className="text-muted mb-0 small mt-2">
                {data.updatedBy
                  ? t('adminObservability.logLevels.lastUpdatedBy', {
                      date: lastUpdated,
                      user: data.updatedBy
                    })
                  : t('adminObservability.logLevels.lastUpdated', {
                      date: lastUpdated
                    })}
              </p>
            )}
          </div>
          <div className="d-flex align-items-center gap-2">
            <span className="text-muted">
              {t('adminObservability.logLevels.globalLabel')}
            </span>
            <Form.Select
              size="sm"
              value={data.global}
              disabled={setGlobalStatus.isLoading}
              onChange={e => handleGlobal(e.target.value as LogLevel)}
              style={{ width: 120 }}
            >
              {LOG_LEVELS.map(l => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </Form.Select>
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={handleResetAll}
              disabled={resetStatus.isLoading}
            >
              {t('adminObservability.logLevels.resetToEnv')}
            </Button>
          </div>
        </Card.Body>
      </Card>

      <Card className="shadow-none border">
        <Card.Body className="p-0">
          <Table responsive hover className="mb-0">
            <thead>
              <tr>
                <th>{t('adminObservability.logLevels.columns.module')}</th>
                <th>{t('adminObservability.logLevels.columns.effective')}</th>
                <th>{t('adminObservability.logLevels.columns.override')}</th>
                <th style={{ width: 220 }}>
                  {t('adminObservability.logLevels.columns.set')}
                </th>
                <th style={{ width: 140 }}>
                  {t('adminObservability.logLevels.columns.actions')}
                </th>
              </tr>
            </thead>
            <tbody>
              {data.modules.length === 0 && (
                <tr>
                  <td colSpan={5} className="text-muted text-center py-4">
                    {t('adminObservability.logLevels.noModules')}
                  </td>
                </tr>
              )}
              {data.modules.map(entry => (
                <tr key={entry.name}>
                  <td>
                    <code>{entry.name}</code>
                  </td>
                  <td>
                    <Badge bg={levelVariant[entry.effective]}>
                      {entry.effective}
                    </Badge>
                  </td>
                  <td>
                    {entry.hasOverride ? (
                      <span className="text-success small">
                        {t('adminObservability.logLevels.overrideExplicit')}
                      </span>
                    ) : (
                      <span className="text-muted small">
                        {t('adminObservability.logLevels.overrideInherits')}
                      </span>
                    )}
                  </td>
                  <td>
                    <Form.Select
                      size="sm"
                      value={entry.effective}
                      onChange={e =>
                        handleModule(entry.name, e.target.value as LogLevel)
                      }
                    >
                      {LOG_LEVELS.map(l => (
                        <option key={l} value={l}>
                          {l}
                        </option>
                      ))}
                    </Form.Select>
                  </td>
                  <td>
                    <Button
                      variant="link"
                      size="sm"
                      disabled={!entry.hasOverride}
                      onClick={() => handleRevert(entry.name)}
                    >
                      {t('adminObservability.logLevels.revert')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card.Body>
      </Card>
    </>
  );
};

export default LogLevelsPage;
