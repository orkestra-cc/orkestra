import { useMemo, useState } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { faSliders } from '@fortawesome/free-solid-svg-icons';
import type { ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import SectionCard from 'components/common/SectionCard';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import { useApplyPermanentLogLevelsMutation } from 'store/api/observabilityApi';
import {
  LOG_LEVELS,
  type AdminModuleEntry,
  type LogLevel,
  type LogLevelsView
} from 'types/observability';

interface PermanentLevelsPanelProps {
  snapshot: LogLevelsView;
  onReload: () => Promise<unknown>;
}

interface PermanentDraft {
  global: LogLevel;
  perModule: Record<string, LogLevel>;
}

interface PermanentEditor {
  baseline: PermanentDraft;
  draft: PermanentDraft;
  expectedUpdatedAt: string;
}

const levelVariant: Record<LogLevel, BadgeColor> = {
  debug: 'secondary',
  info: 'primary',
  warn: 'warning',
  error: 'danger'
};

const draftFromSnapshot = (snapshot: LogLevelsView): PermanentDraft => ({
  global: snapshot.global,
  perModule: Object.fromEntries(
    snapshot.modules
      .filter(module => module.hasOverride)
      .map(module => [module.name, module.effective])
  )
});

const countChanges = (
  baseline: PermanentDraft,
  draft: PermanentDraft
): number => {
  let count = baseline.global === draft.global ? 0 : 1;
  const moduleNames = new Set([
    ...Object.keys(baseline.perModule),
    ...Object.keys(draft.perModule)
  ]);
  for (const moduleName of moduleNames) {
    if (baseline.perModule[moduleName] !== draft.perModule[moduleName]) {
      count += 1;
    }
  }
  return count;
};

const isConflict = (error: unknown): boolean =>
  typeof error === 'object' &&
  error !== null &&
  'status' in error &&
  error.status === 409;

const PermanentLevelsPanel = ({
  snapshot,
  onReload
}: PermanentLevelsPanelProps) => {
  const { t } = useTranslation();
  const initialDraft = draftFromSnapshot(snapshot);
  const [editor, setEditor] = useState<PermanentEditor>({
    baseline: initialDraft,
    draft: initialDraft,
    expectedUpdatedAt: snapshot.updatedAt
  });
  const [applyPermanent, applyStatus] = useApplyPermanentLogLevelsMutation();
  const [conflict, setConflict] = useState(false);
  const [saveError, setSaveError] = useState(false);

  const dirtyCount = countChanges(editor.baseline, editor.draft);
  const hasDebug =
    editor.draft.global === 'debug' ||
    Object.values(editor.draft.perModule).includes('debug');

  const setGlobal = (global: LogLevel) => {
    setEditor(current => ({
      ...current,
      draft: { ...current.draft, global }
    }));
    setConflict(false);
    setSaveError(false);
  };

  const setModule = (moduleName: string, value: string) => {
    setEditor(current => {
      const perModule = { ...current.draft.perModule };
      if (value === 'inherit') {
        delete perModule[moduleName];
      } else {
        perModule[moduleName] = value as LogLevel;
      }
      return {
        ...current,
        draft: { ...current.draft, perModule }
      };
    });
    setConflict(false);
    setSaveError(false);
  };

  const handleDiscard = () => {
    setEditor(current => ({ ...current, draft: current.baseline }));
    setConflict(false);
    setSaveError(false);
  };

  const handleApply = async () => {
    setConflict(false);
    setSaveError(false);
    try {
      const saved = await applyPermanent({
        global: editor.draft.global,
        perModule: editor.draft.perModule,
        expectedUpdatedAt: editor.expectedUpdatedAt
      }).unwrap();
      const savedDraft = draftFromSnapshot(saved);
      setEditor({
        baseline: savedDraft,
        draft: savedDraft,
        expectedUpdatedAt: saved.updatedAt
      });
    } catch (error) {
      if (isConflict(error)) {
        setConflict(true);
      } else {
        setSaveError(true);
      }
    }
  };

  const handleReload = async () => {
    await onReload();
    setConflict(false);
    setSaveError(false);
  };

  const columns = useMemo<ColumnDef<AdminModuleEntry>[]>(
    () => [
      {
        accessorKey: 'name',
        header: t('adminObservability.loggingWorkspace.levels.columns.module'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }) => <code>{original.name}</code>
      },
      {
        id: 'permanent',
        header: t(
          'adminObservability.loggingWorkspace.levels.columns.permanent'
        ),
        enableSorting: false,
        meta: {
          headerProps: { className: 'text-900 text-nowrap' },
          cellProps: { className: 'text-nowrap' }
        },
        cell: ({ row: { original } }) => (
          <Form.Select
            size="sm"
            value={editor.draft.perModule[original.name] ?? 'inherit'}
            aria-label={t(
              'adminObservability.loggingWorkspace.levels.moduleAria',
              { module: original.name }
            )}
            onChange={event => setModule(original.name, event.target.value)}
          >
            <option value="inherit">
              {t('adminObservability.loggingWorkspace.levels.inherit')}
            </option>
            {LOG_LEVELS.map(level => (
              <option key={level} value={level}>
                {t(`adminObservability.loggingWorkspace.levelNames.${level}`)}
              </option>
            ))}
          </Form.Select>
        )
      },
      {
        id: 'inherited',
        header: t(
          'adminObservability.loggingWorkspace.levels.columns.inherited'
        ),
        meta: { headerProps: { className: 'text-900 text-nowrap' } },
        cell: () => (
          <SubtleBadge bg={levelVariant[editor.draft.global]}>
            {t(
              `adminObservability.loggingWorkspace.levelNames.${editor.draft.global}`
            )}
          </SubtleBadge>
        )
      },
      {
        accessorKey: 'effective',
        header: t(
          'adminObservability.loggingWorkspace.levels.columns.effective'
        ),
        meta: { headerProps: { className: 'text-900 text-nowrap' } },
        cell: ({ row: { original } }) => (
          <SubtleBadge bg={levelVariant[original.effective]}>
            {t(
              `adminObservability.loggingWorkspace.levelNames.${original.effective}`
            )}
          </SubtleBadge>
        )
      }
    ],
    [editor.draft, t]
  );

  const table = useAdvanceTable({
    data: snapshot.modules,
    columns,
    sortable: true,
    pagination: false
  });

  return (
    <>
      <SectionCard
        icon={faSliders}
        title={t('adminObservability.loggingWorkspace.levels.title')}
      >
        <p className="text-muted fs-10">
          {t('adminObservability.loggingWorkspace.levels.description')}
        </p>

        <Form.Group className="mb-3" controlId="logging-permanent-global">
          <Form.Label>
            {t('adminObservability.loggingWorkspace.levels.globalLabel')}
          </Form.Label>
          <Form.Select
            className="w-auto"
            value={editor.draft.global}
            aria-label={t(
              'adminObservability.loggingWorkspace.levels.globalAria'
            )}
            onChange={event => setGlobal(event.target.value as LogLevel)}
          >
            {LOG_LEVELS.map(level => (
              <option key={level} value={level}>
                {t(`adminObservability.loggingWorkspace.levelNames.${level}`)}
              </option>
            ))}
          </Form.Select>
          <Form.Text className="d-block">
            {t(
              `adminObservability.loggingWorkspace.levelDescriptions.${editor.draft.global}`
            )}
          </Form.Text>
        </Form.Group>

        {hasDebug && (
          <Alert variant="warning" className="fs-10 py-2">
            {t('adminObservability.loggingWorkspace.levels.debugWarning')}
          </Alert>
        )}

        {snapshot.modules.length === 0 ? (
          <p className="text-muted fs-10 mb-0">
            {t('adminObservability.loggingWorkspace.levels.empty')}
          </p>
        ) : (
          <AdvanceTableProvider {...table}>
            <AdvanceTable
              headerClassName="text-nowrap align-middle"
              rowClassName="align-middle"
              tableProps={{
                striped: true,
                className: 'fs-10 mb-0 overflow-hidden'
              }}
            />
          </AdvanceTableProvider>
        )}
      </SectionCard>

      {conflict && (
        <Alert variant="warning" className="mt-3 fs-10">
          <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
            <span>
              {t('adminObservability.loggingWorkspace.levels.conflict')}
            </span>
            <Button variant="warning" size="sm" onClick={handleReload}>
              {t('adminObservability.loggingWorkspace.levels.reloadSnapshot')}
            </Button>
          </div>
        </Alert>
      )}

      {saveError && (
        <Alert variant="danger" className="mt-3 fs-10">
          {t('adminObservability.loggingWorkspace.levels.saveFailed')}
        </Alert>
      )}

      {dirtyCount > 0 && (
        <div className="position-sticky bottom-0 bg-body border rounded shadow-sm p-3 mt-3 d-flex flex-wrap align-items-center justify-content-between gap-2">
          <span className="fw-semibold fs-10">
            {t('adminObservability.loggingWorkspace.levels.dirtyCount', {
              count: dirtyCount
            })}
          </span>
          <div className="d-flex gap-2">
            <Button
              variant="secondary"
              size="sm"
              disabled={applyStatus.isLoading}
              onClick={handleDiscard}
            >
              {t('adminObservability.loggingWorkspace.levels.discard')}
            </Button>
            <Button
              variant="falcon-primary"
              size="sm"
              disabled={applyStatus.isLoading}
              onClick={handleApply}
            >
              {applyStatus.isLoading && (
                <Spinner animation="border" size="sm" className="me-2" />
              )}
              {t('adminObservability.loggingWorkspace.levels.apply')}
            </Button>
          </div>
        </div>
      )}
    </>
  );
};

export default PermanentLevelsPanel;
