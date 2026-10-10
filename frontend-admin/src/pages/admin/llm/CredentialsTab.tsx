import { useCallback, useMemo, useState } from 'react';
import { Spinner } from 'react-bootstrap';
import {
  faBan,
  faCircleCheck,
  faKey,
  faPen,
  faRotate,
  faTrash
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import EmptyState from 'components/common/EmptyState';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { byTimestamp } from 'components/common/advance-table/sorting';
import { formatDate } from 'helpers/dateFormat';
import {
  useDeleteLlmCredentialMutation,
  useListLlmCredentialsQuery,
  usePatchLlmCredentialMutation,
  type LlmCredential
} from 'store/api/llmApi';
import ConfirmDeleteModal from './ConfirmDeleteModal';
import CredentialEditorModal, {
  type EditorMode
} from './CredentialEditorModal';
import LlmLoadError from './LlmLoadError';
import { isReauthCancelled, llmErrorMessage } from './llmErrors';
import { useLlmPermissions } from './llmPermissions';
import { needsSecret } from './llmRules';

// CredentialsTab lists the org's provider credentials. Create, rotate,
// re-point, enable/disable and delete are step-up-gated on the backend; the
// global StepUpModal replays the request after re-auth. Delete answers 409
// llm.credential_in_use while an active model references the credential.
const CredentialsTab = () => {
  const { t } = useTranslation();
  const { canManageCredentials: canManage } = useLlmPermissions();
  const { data, isLoading, isError, refetch } = useListLlmCredentialsQuery();
  const [remove, { isLoading: deleting }] = useDeleteLlmCredentialMutation();
  const [patch] = usePatchLlmCredentialMutation();
  const [editor, setEditor] = useState<{
    mode: EditorMode;
    credential?: LlmCredential;
  } | null>(null);
  const [confirm, setConfirm] = useState<{
    open: boolean;
    credential?: LlmCredential;
    error?: string | null;
  }>({ open: false });

  const onToggle = useCallback(
    async (c: LlmCredential) => {
      const status = c.status === 'active' ? 'disabled' : 'active';
      try {
        await patch({ uuid: c.uuid, status }).unwrap();
        toast.success(
          t(
            status === 'active'
              ? 'adminLlm.credentials.enabled'
              : 'adminLlm.credentials.disabled'
          )
        );
      } catch (e) {
        if (!isReauthCancelled(e)) toast.error(llmErrorMessage(t, e));
      }
    },
    [patch, t]
  );

  const onConfirmDelete = async () => {
    const c = confirm.credential;
    if (!c) return;
    try {
      await remove(c.uuid).unwrap();
      toast.success(t('adminLlm.credentials.deleteSuccess'));
      setConfirm(prev => ({ ...prev, open: false }));
    } catch (e) {
      if (isReauthCancelled(e)) return;
      setConfirm(prev => ({ ...prev, error: llmErrorMessage(t, e) }));
    }
  };

  // Memoised: AdvanceTable renders each `cell` as a component, so a fresh
  // columns array per render would remount every row's actions (and drop
  // the focus a confirmation modal hands back to its trigger).
  const columns = useMemo<ColumnDef<LlmCredential>[]>(
    () => [
      {
        accessorKey: 'name',
        header: t('adminLlm.credentials.columns.name'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'provider',
        // Translated accessor so the search box matches what is on screen.
        accessorFn: c => t(`adminLlm.providers.${c.provider}`),
        header: t('adminLlm.credentials.columns.provider'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'secret',
        header: t('adminLlm.credentials.columns.secret'),
        enableSorting: false,
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }: CellContext<LlmCredential, unknown>) =>
          original.hasSecret ? (
            <span className="font-monospace">
              {t('adminLlm.credentials.secretTail', {
                last4: original.secretLast4 ?? ''
              })}
            </span>
          ) : (
            <span className="text-600">
              {t('adminLlm.credentials.noSecret')}
            </span>
          )
      },
      {
        id: 'status',
        accessorFn: c => t(`adminLlm.status.${c.status}`),
        header: t('adminLlm.credentials.columns.status'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }: CellContext<LlmCredential, unknown>) => (
          <SubtleBadge
            pill
            bg={original.status === 'active' ? 'success' : 'secondary'}
          >
            {t(`adminLlm.status.${original.status}`)}
          </SubtleBadge>
        )
      },
      {
        id: 'updatedAt',
        // Formatted accessor + timestamp comparator — see byTimestamp.
        accessorFn: c => formatDate(c.updatedAt),
        sortingFn: byTimestamp<LlmCredential>(c => c.updatedAt),
        header: t('adminLlm.credentials.columns.updated'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'actions',
        header: t('adminLlm.columns.actions'),
        enableSorting: false,
        meta: {
          headerProps: { className: 'text-end text-900' },
          cellProps: { className: 'text-end' }
        },
        cell: ({ row: { original } }: CellContext<LlmCredential, unknown>) => {
          const active = original.status === 'active';
          const toggleLabel = t(
            active
              ? 'adminLlm.credentials.disable'
              : 'adminLlm.credentials.enable'
          );
          return (
            <div className="d-inline-flex gap-1">
              <IconButton
                variant="orkestra-default"
                size="sm"
                icon={faPen}
                title={t('adminLlm.credentials.edit')}
                aria-label={t('adminLlm.credentials.edit')}
                disabled={!canManage}
                onClick={() =>
                  setEditor({ mode: 'edit', credential: original })
                }
              />
              {needsSecret(original.provider) && (
                <IconButton
                  variant="orkestra-default"
                  size="sm"
                  icon={faRotate}
                  title={t('adminLlm.credentials.rotate')}
                  aria-label={t('adminLlm.credentials.rotate')}
                  disabled={!canManage}
                  onClick={() =>
                    setEditor({ mode: 'rotate', credential: original })
                  }
                />
              )}
              <IconButton
                variant="orkestra-default"
                size="sm"
                icon={active ? faBan : faCircleCheck}
                title={toggleLabel}
                aria-label={toggleLabel}
                disabled={!canManage}
                onClick={() => onToggle(original)}
              />
              <IconButton
                variant="orkestra-danger"
                size="sm"
                icon={faTrash}
                title={t('adminLlm.credentials.delete')}
                aria-label={t('adminLlm.credentials.delete')}
                disabled={!canManage}
                onClick={() =>
                  setConfirm({ open: true, credential: original, error: null })
                }
              />
            </div>
          );
        }
      }
    ],
    [t, canManage, onToggle]
  );

  if (isLoading) return <Spinner animation="border" size="sm" />;
  if (isError) return <LlmLoadError onRetry={() => refetch()} />;

  const items = data?.items ?? [];
  const openCreate = () => setEditor({ mode: 'create' });
  const noPermission = canManage
    ? undefined
    : t('adminLlm.credentials.noPermission');

  return (
    <>
      {items.length === 0 ? (
        <EmptyState
          icon={faKey}
          message={t('adminLlm.credentials.emptyTitle')}
          hint={noPermission ?? t('adminLlm.credentials.emptyHint')}
          ctaLabel={t('adminLlm.credentials.add')}
          ctaDisabled={!canManage}
          onCta={openCreate}
        />
      ) : (
        <>
          <div className="d-flex justify-content-end mb-3">
            <IconButton
              variant="orkestra-primary"
              size="sm"
              icon={faKey}
              disabled={!canManage}
              title={noPermission}
              onClick={openCreate}
            >
              {t('adminLlm.credentials.add')}
            </IconButton>
          </div>
          <SearchablePagedTable
            data={items}
            columns={columns}
            searchPlaceholder={t('adminLlm.credentials.search')}
          />
        </>
      )}
      {editor && (
        <CredentialEditorModal
          mode={editor.mode}
          credential={editor.credential}
          onClose={() => setEditor(null)}
        />
      )}
      <ConfirmDeleteModal
        show={confirm.open}
        title={t('adminLlm.credentials.deleteTitle')}
        body={t('adminLlm.credentials.deleteConfirm', {
          name: confirm.credential?.name ?? ''
        })}
        error={confirm.error}
        busy={deleting}
        onCancel={() => setConfirm(prev => ({ ...prev, open: false }))}
        onConfirm={onConfirmDelete}
      />
    </>
  );
};

export default CredentialsTab;
