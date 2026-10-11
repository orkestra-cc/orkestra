import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import { Alert, Button, Col, Modal, Row, Spinner } from 'react-bootstrap';
import {
  faBuilding,
  faCircleCheck,
  faLink,
  faLinkSlash,
  faRobot,
  faRotate,
  faUser
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import IconButton from 'components/common/IconButton';
import StatCard from 'components/common/StatCard';
import SubtleBadge from 'components/common/SubtleBadge';
import { byTimestamp } from 'components/common/advance-table/sorting';
import { formatDate, formatDateTime } from 'helpers/dateFormat';
import { useAppSelector } from 'store/hooks';
import {
  useGetMyLlmUsageQuery,
  useLinkLlmAccountMutation,
  useListMyLlmAccountsQuery,
  useListMyLlmModelsQuery,
  useSelectLlmAccountMutation,
  useUnlinkLlmAccountMutation,
  type LlmAccount,
  type LlmModelInfo
} from 'store/api/llmApi';
import {
  isQuietLlmError,
  llmCodeMessage,
  llmErrorMessage
} from 'pages/admin/llm/llmErrors';
import { useLlmPermissions } from 'pages/admin/llm/llmPermissions';
import AiAccountLinkModal, { type LinkWizardState } from './AiAccountLinkModal';
import { LLM_ACCOUNT_STATUS_BADGE as STATUS_BADGE } from './aiAccountStatus';
import SecurityEmptyState from './SecurityEmptyState';
import SecurityTable from './SecurityTable';

// The closed set of ?error= codes the loopback callback redirects with
// (backend handlers/siwc.go), mapped onto the errors.llm.* copy. Anything
// else reads as the backend's own fallback, a failed exchange. A Map, so a
// hostile ?error=__proto__ / constructor never resolves to a prototype
// member.
const CALLBACK_ERROR_CODES: ReadonlyMap<string, string> = new Map([
  ['state_invalid', 'llm.siwc_state_invalid'],
  ['denied', 'llm.siwc_denied'],
  ['token_exchange_failed', 'llm.siwc_exchange_failed'],
  ['id_token_invalid', 'llm.siwc_id_token_invalid'],
  ['disabled', 'llm.siwc_disabled'],
  ['unsupported_deployment', 'llm.siwc_unsupported_deployment']
]);

type Banner = { variant: 'success' | 'danger'; message: string };

// The wizard remembers the org its attempt was started in: the attempt is
// bound to that org, so it is never shown (or completed) under another.
type OrgWizard = LinkWizardState & { orgId: string };

// AiAccountsTab is the user's own Sign in with ChatGPT surface: the ChatGPT
// accounts linked in the active org, which one plan-billed models use, the
// link wizard, and what those models are and cost this month. The page shows
// the tab only with llm.accounts.self; the models and usage sections need
// llm.models.self and llm.usage.self, so each is skipped without its own.
//
// hosted_paste runs the two-step wizard (AiAccountLinkModal); loopback_local
// hands the whole page to OpenAI, whose callback lands back here with
// ?linked=ok or ?error=<code>, drained into a banner below. Select and unlink
// need no step-up (ruling 5.6: the user's own account).
const AiAccountsTab = () => {
  const { t, i18n } = useTranslation();
  const { canListOwnModels, canReadOwnUsage } = useLlmPermissions();
  const { data, isLoading, isError, refetch } = useListMyLlmAccountsQuery();
  const models = useListMyLlmModelsQuery(undefined, {
    skip: !canListOwnModels
  });
  const usage = useGetMyLlmUsageQuery(undefined, { skip: !canReadOwnUsage });
  const [link, { isLoading: linkPending }] = useLinkLlmAccountMutation();
  const [select, { isLoading: selectPending }] = useSelectLlmAccountMutation();
  const [unlink, { isLoading: unlinkPending }] = useUnlinkLlmAccountMutation();
  const [wizard, setWizard] = useState<OrgWizard | null>(null);
  const [banner, setBanner] = useState<Banner | null>(null);
  const [unlinkTarget, setUnlinkTarget] = useState<LlmAccount | null>(null);
  const [unlinkError, setUnlinkError] = useState<string | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  // The org the requests are sent under (baseApi's X-Tenant-ID).
  const orgId =
    useAppSelector(
      s => s.tenant.impersonatedTenantId ?? s.tenant.currentOrgId
    ) ?? '';
  // Read after an await: the org a pending link response would land in.
  const orgRef = useRef(orgId);
  useEffect(() => {
    orgRef.current = orgId;
  }, [orgId]);
  // An org switch keeps this tab mounted (a super_admin holds every
  // permission everywhere): drop a wizard opened in another org, whose
  // pasted callback would otherwise complete under the new one.
  if (wizard && wizard.orgId !== orgId) setWizard(null);

  // Drain the loopback callback's ?linked= / ?error= into a banner and
  // replace the URL, so a refresh does not replay it. The callback always
  // returns on tab=ai; another tab's params are not ours.
  useEffect(() => {
    if (searchParams.get('tab') !== 'ai') return;
    const linked = searchParams.get('linked');
    const error = searchParams.get('error');
    if (!linked && !error) return;
    setBanner(
      linked === 'ok'
        ? {
            variant: 'success',
            message: t('userSecurity.aiAccounts.linked')
          }
        : {
            variant: 'danger',
            message: llmCodeMessage(
              t,
              CALLBACK_ERROR_CODES.get(error ?? '') ??
                'llm.siwc_exchange_failed'
            )
          }
    );
    const next = new URLSearchParams(searchParams);
    next.delete('linked');
    next.delete('error');
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams, t]);

  // Starts a link, or a sign-in again to `reauth`. Nothing is stored until
  // the attempt completes, so an abandoned wizard leaves nothing behind.
  const startLink = useCallback(
    async (reauth?: LlmAccount) => {
      setBanner(null);
      const startedIn = orgId;
      try {
        const res = await link(
          reauth ? { reauthAccountUuid: reauth.uuid } : {}
        ).unwrap();
        // The org changed while the link was pending: the attempt belongs
        // to the previous org, so it is neither opened nor shown.
        if (orgRef.current !== startedIn) return;
        if (res.mode === 'loopback_local') {
          window.location.assign(res.authorizeUrl);
          return;
        }
        setWizard({
          step: 1,
          authorizeUrl: res.authorizeUrl,
          reauthLabel: reauth?.label,
          orgId: startedIn
        });
      } catch (e) {
        if (orgRef.current !== startedIn) return;
        if (!isQuietLlmError(e)) {
          setBanner({ variant: 'danger', message: llmErrorMessage(t, e) });
        }
      }
    },
    [link, orgId, t]
  );

  const onSelect = useCallback(
    async (account: LlmAccount) => {
      setBanner(null);
      try {
        await select(account.uuid).unwrap();
        setBanner({
          variant: 'success',
          message: t('userSecurity.aiAccounts.selectedNow', {
            label: account.label
          })
        });
      } catch (e) {
        if (!isQuietLlmError(e)) {
          setBanner({ variant: 'danger', message: llmErrorMessage(t, e) });
        }
      }
    },
    [select, t]
  );

  const onConfirmUnlink = async () => {
    if (!unlinkTarget) return;
    setUnlinkError(null);
    try {
      await unlink(unlinkTarget.uuid).unwrap();
      setBanner({
        variant: 'success',
        message: t('userSecurity.aiAccounts.unlinked', {
          label: unlinkTarget.label
        })
      });
      setUnlinkTarget(null);
    } catch (e) {
      if (!isQuietLlmError(e)) setUnlinkError(llmErrorMessage(t, e));
    }
  };

  const disabled = data?.mode === 'disabled';
  const busy = linkPending || selectPending;

  // Memoised: AdvanceTable renders each `cell` as a component, so a fresh
  // columns array per render would remount every row's actions.
  const columns = useMemo<ColumnDef<LlmAccount>[]>(
    () => [
      {
        accessorKey: 'label',
        header: t('userSecurity.aiAccounts.columns.account'),
        cell: ({ row: { original } }: CellContext<LlmAccount, unknown>) => (
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
        accessorFn: a => t(`userSecurity.aiAccounts.status.${a.status}`),
        header: t('userSecurity.aiAccounts.columns.status'),
        cell: ({ row: { original } }: CellContext<LlmAccount, unknown>) => {
          // A non-active state says what to do about it; an active account
          // shows its last recorded failure, a translated llm.* code.
          const note =
            original.status !== 'active'
              ? t(`userSecurity.aiAccounts.remedies.${original.status}`)
              : original.lastError
                ? llmCodeMessage(t, original.lastError)
                : null;
          return (
            <>
              <SubtleBadge pill bg={STATUS_BADGE[original.status]}>
                {t(`userSecurity.aiAccounts.status.${original.status}`)}
              </SubtleBadge>
              {note && (
                <div className="fs-11 text-600 text-wrap mt-1">{note}</div>
              )}
            </>
          );
        }
      },
      {
        id: 'selected',
        accessorFn: a => (a.selected ? 1 : 0),
        header: t('userSecurity.aiAccounts.columns.selected'),
        cell: ({ row: { original } }: CellContext<LlmAccount, unknown>) =>
          original.selected ? (
            <SubtleBadge pill bg="primary">
              {t('userSecurity.aiAccounts.selectedBadge')}
            </SubtleBadge>
          ) : null
      },
      {
        id: 'linkedAt',
        // Formatted accessor + timestamp comparator — see byTimestamp.
        accessorFn: a => formatDate(a.linkedAt),
        sortingFn: byTimestamp<LlmAccount>(a => a.linkedAt),
        header: t('userSecurity.aiAccounts.columns.linked')
      },
      {
        id: 'lastUsedAt',
        accessorFn: a =>
          a.lastUsedAt
            ? formatDateTime(a.lastUsedAt)
            : t('userSecurity.aiAccounts.neverUsed'),
        sortingFn: byTimestamp<LlmAccount>(a => a.lastUsedAt),
        header: t('userSecurity.aiAccounts.columns.lastUsed')
      },
      {
        id: 'actions',
        header: t('userSecurity.aiAccounts.columns.actions'),
        enableSorting: false,
        meta: {
          headerProps: { className: 'text-end' },
          cellProps: { className: 'text-end' }
        },
        cell: ({ row: { original } }: CellContext<LlmAccount, unknown>) => (
          <div className="d-inline-flex gap-1">
            {!disabled &&
              !original.selected &&
              original.status === 'active' && (
                <IconButton
                  size="sm"
                  variant="orkestra-default"
                  icon={faCircleCheck}
                  disabled={busy}
                  onClick={() => onSelect(original)}
                >
                  {t('userSecurity.aiAccounts.actions.select')}
                </IconButton>
              )}
            {!disabled &&
              (original.status === 'needs_reauth' ||
                original.status === 'plan_disabled') && (
                <IconButton
                  size="sm"
                  variant="orkestra-default"
                  icon={faRotate}
                  disabled={busy}
                  onClick={() => startLink(original)}
                >
                  {t('userSecurity.aiAccounts.actions.reauthorize')}
                </IconButton>
              )}
            <IconButton
              size="sm"
              variant="orkestra-default"
              icon={faLinkSlash}
              onClick={() => {
                setUnlinkError(null);
                setUnlinkTarget(original);
              }}
            >
              {t('userSecurity.aiAccounts.actions.unlink')}
            </IconButton>
          </div>
        )
      }
    ],
    [t, disabled, busy, onSelect, startLink]
  );

  const modelColumns = useMemo<ColumnDef<LlmModelInfo>[]>(
    () => [
      {
        accessorKey: 'name',
        header: t('userSecurity.aiAccounts.myModels.columns.name'),
        cell: ({ row: { original } }: CellContext<LlmModelInfo, unknown>) => (
          <span className="fw-semibold text-900">{original.name}</span>
        )
      },
      {
        accessorKey: 'modelId',
        header: t('userSecurity.aiAccounts.myModels.columns.modelId'),
        cell: ({ row: { original } }: CellContext<LlmModelInfo, unknown>) => (
          <span className="font-monospace text-700">{original.modelId}</span>
        )
      }
    ],
    [t]
  );

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }
  if (isError || !data) {
    return (
      <Alert
        variant="danger"
        className="d-flex flex-wrap align-items-center justify-content-between gap-2 fs-10"
      >
        <span>{t('userSecurity.aiAccounts.loadError')}</span>
        <Button variant="orkestra-default" size="sm" onClick={() => refetch()}>
          {t('userSecurity.aiAccounts.retry')}
        </Button>
      </Alert>
    );
  }

  const items = data.items ?? [];
  const planModels = (models.data?.items ?? []).filter(
    m => m.credentialKind === 'user_account'
  );
  const num = (n: number) => n.toLocaleString(i18n.language);
  const mine = usage.data;

  return (
    <>
      <div className="d-flex justify-content-between align-items-start flex-wrap gap-2 mb-3">
        <p className="fs-10 text-muted mb-0">
          {t('userSecurity.aiAccounts.intro')}
        </p>
        {!disabled && (
          <div className="text-end">
            <IconButton
              variant="orkestra-primary"
              size="sm"
              icon={faLink}
              className="text-nowrap"
              disabled={busy}
              onClick={() => startLink()}
            >
              {linkPending
                ? t('userSecurity.aiAccounts.linkStarting')
                : t('userSecurity.aiAccounts.link')}
            </IconButton>
            {data.mode === 'loopback_local' && (
              <div>
                <Button
                  variant="link"
                  size="sm"
                  className="p-0 fs-11"
                  onClick={() => setWizard({ step: 2, orgId })}
                >
                  {t('userSecurity.aiAccounts.pasteInstead')}
                </Button>
              </div>
            )}
          </div>
        )}
      </div>

      {banner && (
        <Alert
          variant={banner.variant}
          dismissible
          onClose={() => setBanner(null)}
          className="fs-10"
        >
          {banner.message}
        </Alert>
      )}

      {disabled && items.length > 0 && (
        <Alert variant="info" className="fs-10">
          {t('userSecurity.aiAccounts.disabledWithAccounts')}
        </Alert>
      )}

      {items.length === 0 ? (
        disabled ? (
          <SecurityEmptyState
            icon={faRobot}
            message={t('userSecurity.aiAccounts.disabled')}
            hint={t('userSecurity.aiAccounts.disabledHint')}
          />
        ) : (
          <SecurityEmptyState
            icon={faRobot}
            message={t('userSecurity.aiAccounts.empty')}
            hint={t('userSecurity.aiAccounts.emptyHint')}
          />
        )
      ) : (
        // A user links a handful of accounts at most: compact, like the
        // linked sign-ins.
        <SecurityTable data={items} columns={columns} compact />
      )}

      {canListOwnModels && (
        <section className="mt-4">
          <h6 className="mb-1">
            {t('userSecurity.aiAccounts.myModels.title')}
          </h6>
          <p className="fs-11 text-600 mb-2">
            {t('userSecurity.aiAccounts.myModels.note')}
          </p>
          {models.isLoading ? (
            <Spinner animation="border" size="sm" />
          ) : models.isError ? (
            <p className="fs-10 text-600 mb-0">
              {t('userSecurity.aiAccounts.myModels.loadError')}
            </p>
          ) : planModels.length === 0 ? (
            <p className="fs-10 text-600 mb-0">
              {t('userSecurity.aiAccounts.myModels.empty')}
            </p>
          ) : (
            <SecurityTable data={planModels} columns={modelColumns} compact />
          )}
        </section>
      )}

      {canReadOwnUsage && (
        <section className="mt-4">
          <h6 className="mb-2">{t('userSecurity.aiAccounts.myUsage.title')}</h6>
          <Row className="g-3">
            <Col md={6}>
              <StatCard
                title={t('userSecurity.aiAccounts.myUsage.org')}
                value={
                  usage.isError || !mine
                    ? t('userSecurity.stats.dash')
                    : num(mine.orgInputTokens + mine.orgOutputTokens)
                }
                icon={faBuilding}
                color="secondary"
                subtitle={
                  usage.isError
                    ? t('userSecurity.stats.unavailable')
                    : t('userSecurity.aiAccounts.myUsage.orgSubtitle')
                }
                loading={usage.isLoading}
              />
            </Col>
            <Col md={6}>
              <StatCard
                title={t('userSecurity.aiAccounts.myUsage.userAccount')}
                value={
                  usage.isError || !mine
                    ? t('userSecurity.stats.dash')
                    : num(
                        mine.userAccountInputTokens +
                          mine.userAccountOutputTokens
                      )
                }
                icon={faUser}
                color="primary"
                subtitle={
                  usage.isError
                    ? t('userSecurity.stats.unavailable')
                    : t('userSecurity.aiAccounts.myUsage.userAccountSubtitle')
                }
                loading={usage.isLoading}
              />
            </Col>
          </Row>
        </section>
      )}

      {wizard && (
        <AiAccountLinkModal
          state={wizard}
          onStep={step => setWizard(prev => (prev ? { ...prev, step } : prev))}
          onClose={() => setWizard(null)}
          onLinked={account => {
            setWizard(null);
            setBanner({
              variant: 'success',
              message: t('userSecurity.aiAccounts.linkedNamed', {
                label: account.label
              })
            });
          }}
        />
      )}

      <Modal
        show={!!unlinkTarget}
        onHide={() => setUnlinkTarget(null)}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('userSecurity.aiAccounts.unlinkTitle', {
              label: unlinkTarget?.label ?? ''
            })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {unlinkError && (
            <Alert variant="danger" className="fs-10 py-2">
              {unlinkError}
            </Alert>
          )}
          <p className="mb-0">{t('userSecurity.aiAccounts.unlinkBody')}</p>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="orkestra-default"
            onClick={() => setUnlinkTarget(null)}
          >
            {t('userSecurity.aiAccounts.wizard.cancel')}
          </Button>
          <Button
            variant="orkestra-danger"
            disabled={unlinkPending}
            onClick={onConfirmUnlink}
          >
            {t('userSecurity.aiAccounts.actions.unlink')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default AiAccountsTab;
