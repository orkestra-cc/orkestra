import { useCallback, useMemo } from 'react';
import { Alert, Button, Card, Form } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { toast } from 'react-toastify';
import { useTranslation } from 'react-i18next';
import { ColumnDef } from '@tanstack/react-table';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import {
  useGetLogLevelsQuery,
  useSetGlobalLogLevelMutation,
  useSetModuleLogLevelMutation,
  useUnsetModuleLogLevelMutation,
  useResetLogLevelsMutation
} from 'store/api/observabilityApi';
import {
  LOG_LEVELS,
  type AdminModuleEntry,
  type LogLevel
} from 'types/observability';

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

const levelVariant: Record<LogLevel, BadgeColor> = {
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

  // Both handlers are called from column definitions, so they need a stable
  // identity for the useMemo below to be worth anything.
  const handleModule = useCallback(
    async (moduleName: string, level: LogLevel) => {
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
    },
    [setModule, t]
  );

  const handleRevert = useCallback(
    async (moduleName: string) => {
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
    },
    [unsetModule, t]
  );

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

  const columns = useMemo<ColumnDef<AdminModuleEntry>[]>(
    () => [
      {
        accessorKey: 'name',
        header: t('adminObservability.logLevels.columns.module'),
        cell: ({ row: { original } }) => <code>{original.name}</code>
      },
      {
        accessorKey: 'effective',
        header: t('adminObservability.logLevels.columns.effective'),
        cell: ({ row: { original } }) => (
          <SubtleBadge bg={levelVariant[original.effective]}>
            {original.effective}
          </SubtleBadge>
        )
      },
      {
        accessorKey: 'hasOverride',
        header: t('adminObservability.logLevels.columns.override'),
        cell: ({ row: { original } }) =>
          original.hasOverride ? (
            <span className="text-success small">
              {t('adminObservability.logLevels.overrideExplicit')}
            </span>
          ) : (
            <span className="text-muted small">
              {t('adminObservability.logLevels.overrideInherits')}
            </span>
          )
      },
      {
        id: 'set',
        header: t('adminObservability.logLevels.columns.set'),
        enableSorting: false,
        meta: { headerProps: { style: { width: 220 } } },
        cell: ({ row: { original } }) => (
          <Form.Select
            size="sm"
            value={original.effective}
            // Per-row control: without this the screen reader announces four
            // identical unlabelled comboboxes (WCAG 4.1.2).
            aria-label={t('adminObservability.logLevels.setAria', {
              module: original.name
            })}
            onChange={e =>
              handleModule(original.name, e.target.value as LogLevel)
            }
          >
            {LOG_LEVELS.map(l => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </Form.Select>
        )
      },
      {
        id: 'actions',
        header: t('adminObservability.logLevels.columns.actions'),
        enableSorting: false,
        meta: { headerProps: { style: { width: 140 } } },
        cell: ({ row: { original } }) => (
          <Button
            variant="link"
            size="sm"
            className="p-0"
            disabled={!original.hasOverride}
            onClick={() => handleRevert(original.name)}
            aria-label={t('adminObservability.logLevels.revertAria', {
              module: original.name
            })}
          >
            {t('adminObservability.logLevels.revert')}
          </Button>
        )
      }
    ],
    [t, handleModule, handleRevert]
  );

  const table = useAdvanceTable({
    data: data?.modules ?? [],
    columns,
    sortable: true,
    pagination: false
  });

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
            <Form.Label htmlFor="global-log-level" className="text-muted mb-0">
              {t('adminObservability.logLevels.globalLabel')}
            </Form.Label>
            <Form.Select
              id="global-log-level"
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
        {data.modules.length === 0 ? (
          <Card.Body className="text-muted text-center py-4">
            {t('adminObservability.logLevels.noModules')}
          </Card.Body>
        ) : (
          <Card.Body className="p-0">
            <AdvanceTableProvider {...table}>
              <AdvanceTable
                headerClassName="bg-200 text-nowrap align-middle"
                rowClassName="align-middle"
                tableProps={{
                  size: 'sm',
                  className: 'fs-10 mb-0 overflow-hidden'
                }}
              />
            </AdvanceTableProvider>
          </Card.Body>
        )}
      </Card>
    </>
  );
};

export default LogLevelsPage;
