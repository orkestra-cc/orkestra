import { useCallback, useMemo, useState } from 'react';
import { Spinner } from 'react-bootstrap';
import {
  faBan,
  faCircleCheck,
  faPen,
  faRobot,
  faTrash,
  faUsers
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import EmptyState from 'components/common/EmptyState';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import {
  useDeleteLlmModelMutation,
  useListLlmCredentialsQuery,
  useListLlmModelsQuery,
  usePatchLlmModelMutation,
  type LlmModel
} from 'store/api/llmApi';
import ConfirmDeleteModal from './ConfirmDeleteModal';
import LlmLoadError from './LlmLoadError';
import ModelEditorModal from './ModelEditorModal';
import { isReauthCancelled, llmErrorMessage } from './llmErrors';
import { useLlmPermissions } from './llmPermissions';

// ModelsTab lists the org's configured models with who may use them.
// Defining a model (llm.models.admin) and deciding its users
// (llm.grants.admin) are separate permissions, so the row actions are gated
// one by one. Disabling a model is always possible and keeps its grants;
// delete removes the grants too.
const ModelsTab = () => {
  const { t } = useTranslation();
  const { canManageModels, canManageGrants } = useLlmPermissions();
  const { data, isLoading, isError, refetch } = useListLlmModelsQuery();
  const credentials = useListLlmCredentialsQuery();
  const [remove, { isLoading: deleting }] = useDeleteLlmModelMutation();
  const [patch] = usePatchLlmModelMutation();
  const [editor, setEditor] = useState<{
    model?: LlmModel;
    grantsOnly?: boolean;
  } | null>(null);
  const [confirm, setConfirm] = useState<{
    open: boolean;
    model?: LlmModel;
    error?: string | null;
  }>({ open: false });

  const onToggle = useCallback(
    async (m: LlmModel) => {
      const status = m.status === 'active' ? 'disabled' : 'active';
      try {
        await patch({ uuid: m.uuid, status }).unwrap();
        toast.success(
          t(
            status === 'active'
              ? 'adminLlm.models.enabled'
              : 'adminLlm.models.disabled'
          )
        );
      } catch (e) {
        if (!isReauthCancelled(e)) toast.error(llmErrorMessage(t, e));
      }
    },
    [patch, t]
  );

  const onConfirmDelete = async () => {
    const m = confirm.model;
    if (!m) return;
    try {
      await remove(m.uuid).unwrap();
      toast.success(t('adminLlm.models.deleteSuccess'));
      setConfirm(prev => ({ ...prev, open: false }));
    } catch (e) {
      if (isReauthCancelled(e)) return;
      setConfirm(prev => ({ ...prev, error: llmErrorMessage(t, e) }));
    }
  };

  // Memoised: AdvanceTable renders each `cell` as a component, so a fresh
  // columns array per render would remount every row's actions (and drop
  // the focus a confirmation modal hands back to its trigger).
  const columns = useMemo<ColumnDef<LlmModel>[]>(
    () => [
      {
        accessorKey: 'name',
        header: t('adminLlm.models.columns.name'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'model',
        accessorFn: m =>
          `${t(`adminLlm.providers.${m.provider}`)} ${m.modelId}`,
        header: t('adminLlm.models.columns.model'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }: CellContext<LlmModel, unknown>) => (
          <span>
            {t(`adminLlm.providers.${original.provider}`)}
            <span className="font-monospace ms-2">{original.modelId}</span>
          </span>
        )
      },
      {
        id: 'billing',
        accessorFn: m => t(`adminLlm.credentialKind.${m.credentialRef.kind}`),
        header: t('adminLlm.models.columns.billing'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'purposes',
        accessorFn: m =>
          m.purposes
            .map(p =>
              t('adminLlm.models.purposeWithPriority', {
                purpose: p.purpose,
                priority: p.priority
              })
            )
            .join(', '),
        header: t('adminLlm.models.columns.purposes'),
        enableSorting: false,
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'access',
        accessorFn: m =>
          m.access === 'everyone'
            ? t('adminLlm.access.everyone')
            : String(m.grants.length),
        header: t('adminLlm.models.columns.access'),
        enableSorting: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }: CellContext<LlmModel, unknown>) =>
          original.access === 'everyone' ? (
            <SubtleBadge pill bg="info">
              {t('adminLlm.access.everyone')}
            </SubtleBadge>
          ) : (
            <span data-testid="llm-grant-count">{original.grants.length}</span>
          )
      },
      {
        id: 'status',
        accessorFn: m => t(`adminLlm.status.${m.status}`),
        header: t('adminLlm.models.columns.status'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }: CellContext<LlmModel, unknown>) => (
          <SubtleBadge
            pill
            bg={original.status === 'active' ? 'success' : 'secondary'}
          >
            {t(`adminLlm.status.${original.status}`)}
          </SubtleBadge>
        )
      },
      {
        id: 'actions',
        header: t('adminLlm.columns.actions'),
        enableSorting: false,
        meta: {
          headerProps: { className: 'text-end text-900' },
          cellProps: { className: 'text-end' }
        },
        cell: ({ row: { original } }: CellContext<LlmModel, unknown>) => {
          const active = original.status === 'active';
          const toggleLabel = t(
            active ? 'adminLlm.models.disable' : 'adminLlm.models.enable'
          );
          return (
            <div className="d-inline-flex gap-1">
              <IconButton
                variant="orkestra-default"
                size="sm"
                icon={faUsers}
                title={
                  original.access === 'everyone'
                    ? t('adminLlm.models.grantsEveryone')
                    : t('adminLlm.models.grants')
                }
                aria-label={t('adminLlm.models.grants')}
                disabled={!canManageGrants}
                onClick={() => setEditor({ model: original, grantsOnly: true })}
              />
              <IconButton
                variant="orkestra-default"
                size="sm"
                icon={faPen}
                title={t('adminLlm.models.edit')}
                aria-label={t('adminLlm.models.edit')}
                disabled={!canManageModels}
                onClick={() => setEditor({ model: original })}
              />
              <IconButton
                variant="orkestra-default"
                size="sm"
                icon={active ? faBan : faCircleCheck}
                title={toggleLabel}
                aria-label={toggleLabel}
                disabled={!canManageModels}
                onClick={() => onToggle(original)}
              />
              <IconButton
                variant="orkestra-danger"
                size="sm"
                icon={faTrash}
                title={t('adminLlm.models.delete')}
                aria-label={t('adminLlm.models.delete')}
                disabled={!canManageModels}
                onClick={() =>
                  setConfirm({ open: true, model: original, error: null })
                }
              />
            </div>
          );
        }
      }
    ],
    [t, canManageModels, canManageGrants, onToggle]
  );

  if (isLoading || credentials.isLoading) {
    return <Spinner animation="border" size="sm" />;
  }
  if (isError || credentials.isError) {
    return (
      <LlmLoadError
        onRetry={() => {
          if (isError) refetch();
          if (credentials.isError) credentials.refetch();
        }}
      />
    );
  }

  const items = data?.items ?? [];
  const noCredentials = (credentials.data?.items ?? []).length === 0;
  const canAdd = canManageModels && !noCredentials;
  const blockedReason = !canManageModels
    ? t('adminLlm.models.noPermission')
    : noCredentials
      ? t('adminLlm.models.needCredential')
      : undefined;
  const openCreate = () => setEditor({});

  return (
    <>
      {items.length === 0 ? (
        <EmptyState
          icon={faRobot}
          message={t('adminLlm.models.emptyTitle')}
          hint={blockedReason ?? t('adminLlm.models.emptyHint')}
          ctaLabel={t('adminLlm.models.add')}
          ctaDisabled={!canAdd}
          onCta={openCreate}
        />
      ) : (
        <>
          <div className="d-flex justify-content-end mb-3">
            <IconButton
              variant="orkestra-primary"
              size="sm"
              icon={faRobot}
              disabled={!canAdd}
              title={blockedReason}
              onClick={openCreate}
            >
              {t('adminLlm.models.add')}
            </IconButton>
          </div>
          <SearchablePagedTable
            data={items}
            columns={columns}
            searchPlaceholder={t('adminLlm.models.search')}
          />
        </>
      )}
      {editor && (
        <ModelEditorModal
          model={editor.model}
          grantsOnly={editor.grantsOnly}
          credentials={credentials.data?.items ?? []}
          onClose={() => setEditor(null)}
        />
      )}
      <ConfirmDeleteModal
        show={confirm.open}
        title={t('adminLlm.models.deleteTitle')}
        body={t('adminLlm.models.deleteConfirm', {
          name: confirm.model?.name ?? ''
        })}
        error={confirm.error}
        busy={deleting}
        onCancel={() => setConfirm(prev => ({ ...prev, open: false }))}
        onConfirm={onConfirmDelete}
      />
    </>
  );
};

export default ModelsTab;
