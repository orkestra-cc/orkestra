import { useCallback, useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Alert, Spinner } from 'react-bootstrap';
import { faLinkSlash, faRobot } from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import EmptyState from 'components/common/EmptyState';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { byTimestamp } from 'components/common/advance-table/sorting';
import { formatDateTime } from 'helpers/dateFormat';
import {
  useForceUnlinkLlmAccountMutation,
  useListLlmAccountsAdminQuery,
  type LlmAccountAdmin
} from 'store/api/llmApi';
import { useListOrgMembersAdminQuery } from 'store/api/tenantApi';
import { useAppSelector } from 'store/hooks';
import { LLM_ACCOUNT_STATUS_BADGE as STATUS_BADGE } from 'pages/user/security/aiAccountStatus';
import ConfirmDeleteModal from './ConfirmDeleteModal';
import LlmLoadError from './LlmLoadError';
import { isReauthCancelled, llmErrorMessage } from './llmErrors';
import { useLlmPermissions } from './llmPermissions';

// AccountsTab lists the ChatGPT accounts the org's members linked with
// Sign in with ChatGPT (llm.admin.read), with the installation's mode read
// from the same response (ruling 2.18) — never from the module config API,
// which needs another permission. The list carries no token material.
// Force unlink (llm.accounts.admin + MFA + step-up; the global StepUpModal
// replays it) revokes the member's sign-in at OpenAI and removes it.
const AccountsTab = () => {
  const { t } = useTranslation();
  const { canManageAccounts: canManage } = useLlmPermissions();
  const { data, isLoading, isError, refetch } = useListLlmAccountsAdminQuery();
  const [forceUnlink, { isLoading: unlinking }] =
    useForceUnlinkLlmAccountMutation();
  const [confirm, setConfirm] = useState<{
    open: boolean;
    account?: LlmAccountAdmin;
    error?: string | null;
  }>({ open: false });
  const tenantId =
    useAppSelector(
      s => s.tenant.impersonatedTenantId ?? s.tenant.currentOrgId
    ) ?? '';

  // Members' emails label the owner column. GET …/members needs
  // system.tenants.admin on top of llm.admin.read; without it (or before it
  // loads) the owner shows by id.
  const members = useListOrgMembersAdminQuery(tenantId, { skip: !tenantId });
  const emailOf = useMemo(
    () =>
      new Map((members.data?.members ?? []).map(m => [m.userUUID, m.email])),
    [members.data]
  );
  const ownerOf = useCallback(
    (a: LlmAccountAdmin) => emailOf.get(a.userUuid) || a.userUuid,
    [emailOf]
  );

  const columns = useMemo<ColumnDef<LlmAccountAdmin>[]>(
    () => [
      {
        id: 'user',
        accessorFn: a => ownerOf(a),
        header: t('adminLlm.accounts.columns.user'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'account',
        accessorFn: a => `${a.label} ${a.email ?? ''}`,
        header: t('adminLlm.accounts.columns.account'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<LlmAccountAdmin, unknown>) => (
          <>
            <span className="fw-semibold text-900">{original.label}</span>
            {original.email && original.email !== original.label && (
              <div className="fs-11 text-600">{original.email}</div>
            )}
          </>
        )
      },
      {
        id: 'status',
        accessorFn: a => t(`adminLlm.accounts.status.${a.status}`),
        header: t('adminLlm.accounts.columns.status'),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({
          row: { original }
        }: CellContext<LlmAccountAdmin, unknown>) => (
          <SubtleBadge pill bg={STATUS_BADGE[original.status]}>
            {t(`adminLlm.accounts.status.${original.status}`)}
          </SubtleBadge>
        )
      },
      {
        id: 'planUsage',
        accessorFn: a =>
          t(
            a.planUsageGranted
              ? 'adminLlm.accounts.planGranted'
              : 'adminLlm.accounts.planNotGranted'
          ),
        header: t('adminLlm.accounts.columns.planUsage'),
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'lastUsedAt',
        // Formatted accessor + timestamp comparator — see byTimestamp.
        accessorFn: a =>
          a.lastUsedAt
            ? formatDateTime(a.lastUsedAt)
            : t('adminLlm.accounts.neverUsed'),
        sortingFn: byTimestamp<LlmAccountAdmin>(a => a.lastUsedAt),
        header: t('adminLlm.accounts.columns.lastUsed'),
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
        cell: ({
          row: { original }
        }: CellContext<LlmAccountAdmin, unknown>) => (
          <IconButton
            variant="orkestra-danger"
            size="sm"
            icon={faLinkSlash}
            title={
              canManage
                ? t('adminLlm.accounts.forceUnlink')
                : t('adminLlm.accounts.noPermission')
            }
            aria-label={t('adminLlm.accounts.forceUnlink')}
            disabled={!canManage}
            onClick={() =>
              setConfirm({ open: true, account: original, error: null })
            }
          />
        )
      }
    ],
    [t, canManage, ownerOf]
  );

  const onConfirm = async () => {
    const a = confirm.account;
    if (!a) return;
    try {
      await forceUnlink(a.uuid).unwrap();
      toast.success(t('adminLlm.accounts.forceUnlinkSuccess'));
      setConfirm(prev => ({ ...prev, open: false }));
    } catch (e) {
      if (isReauthCancelled(e)) return;
      setConfirm(prev => ({ ...prev, error: llmErrorMessage(t, e) }));
    }
  };

  if (isLoading) return <Spinner animation="border" size="sm" />;
  if (isError || !data) return <LlmLoadError onRetry={() => refetch()} />;

  const items = data.items ?? [];
  const disabled = data.mode === 'disabled';

  return (
    <>
      <p className="fs-10 text-muted mb-2">{t('adminLlm.accounts.intro')}</p>
      <Alert
        variant={disabled ? 'warning' : 'info'}
        className="fs-10 py-2"
        data-testid="llm-siwc-mode"
      >
        <span className="fw-semibold">
          {t('adminLlm.accounts.mode', {
            mode: t(`adminLlm.accounts.modes.${data.mode}`)
          })}
        </span>
        {disabled && (
          <>
            {' '}
            {t('adminLlm.accounts.disabledHint')}{' '}
            <Link to="/admin/modules/llm">
              {t('adminLlm.accounts.openSettings')}
            </Link>
          </>
        )}
      </Alert>
      {items.length === 0 ? (
        <EmptyState
          icon={faRobot}
          message={t('adminLlm.accounts.emptyTitle')}
          hint={t('adminLlm.accounts.emptyHint')}
        />
      ) : (
        <SearchablePagedTable
          data={items}
          columns={columns}
          searchPlaceholder={t('adminLlm.accounts.search')}
        />
      )}
      <ConfirmDeleteModal
        show={confirm.open}
        title={t('adminLlm.accounts.forceUnlinkTitle', {
          label: confirm.account?.label ?? ''
        })}
        body={t('adminLlm.accounts.forceUnlinkBody', {
          label: confirm.account?.label ?? '',
          user: confirm.account ? ownerOf(confirm.account) : ''
        })}
        error={confirm.error}
        busy={unlinking}
        confirmLabel={t('adminLlm.accounts.forceUnlink')}
        onCancel={() => setConfirm(prev => ({ ...prev, open: false }))}
        onConfirm={onConfirm}
      />
    </>
  );
};

export default AccountsTab;
