import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import {
  Alert,
  Button,
  ButtonGroup,
  Dropdown,
  Modal,
  Spinner
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { faLink, faLinkSlash } from '@fortawesome/free-solid-svg-icons';
import {
  faApple,
  faDiscord,
  faGithub,
  faGoogle
} from '@fortawesome/free-brands-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import IconButton from 'components/common/IconButton';
import SubtleBadge from 'components/common/SubtleBadge';
import { formatDate } from 'helpers/dateFormat';
import { useTranslation } from 'react-i18next';
import {
  useGetSelfAuthMethodsQuery,
  useInitiateOauthLinkSelfMutation,
  useUnlinkOauthSelfMutation,
  type OAuthProvider,
  type SelfAuthOAuthProvider
} from 'store/api/authApi';
import SecurityEmptyState from './SecurityEmptyState';
import SecurityTable, { byTimestamp } from './SecurityTable';

// Provider brand names — proper nouns, intentionally not translated.
const PROVIDER_LABELS: Record<OAuthProvider, string> = {
  google: 'Google',
  apple: 'Apple',
  github: 'GitHub',
  discord: 'Discord'
};

// Brand glyphs render in the neutral ink, not in each vendor's brand hex:
// components never carry hex values (DESIGN.md, The Utility-Class Rule), and
// the provider's name beside the mark already carries the identity.
const PROVIDER_ICONS: Record<OAuthProvider, IconProp> = {
  google: faGoogle,
  apple: faApple,
  github: faGithub,
  discord: faDiscord
};

const ALL_PROVIDERS: OAuthProvider[] = ['google', 'apple', 'github', 'discord'];

const LINK_FAILURE_CODES = [
  'already_linked',
  'duplicate_provider',
  'invalid_userinfo',
  'internal'
] as const;
type LinkFailureCode = (typeof LINK_FAILURE_CODES)[number];

function isKnownFailure(code: string | undefined): code is LinkFailureCode {
  return !!code && (LINK_FAILURE_CODES as readonly string[]).includes(code);
}

// LinkedProvidersTab lists the OAuth identities the user has linked
// and exposes a per-row Unlink action. The unlink endpoint is gated
// server-side by RequireStepUp(5m); the global StepUpModal pauses
// the request, drives the user through /mfa/verify, and replays.
//
// The list runs through SecurityTable in `compact` mode: the four supported
// IdPs are the hard ceiling on row count, so search and pagination would be
// chrome that never earns its line.
const LinkedProvidersTab = () => {
  const { t } = useTranslation();
  const { data, isLoading, isFetching, refetch } = useGetSelfAuthMethodsQuery();
  const [unlink, { isLoading: unlinkPending }] = useUnlinkOauthSelfMutation();
  const [initiateLink, { isLoading: linkPending }] =
    useInitiateOauthLinkSelfMutation();
  const [target, setTarget] = useState<SelfAuthOAuthProvider | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [linkBanner, setLinkBanner] = useState<{
    kind: 'success' | 'failed';
    provider?: string;
    code?: string;
  } | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  // Drain the link=... query params left by the OAuth callback into a
  // banner + refetch so the user lands on /user/security?tab=oauth and
  // sees the outcome of the round-trip. The query params are consumed
  // (replaced with a clean URL) so a refresh doesn't re-fire the
  // banner.
  useEffect(() => {
    const link = searchParams.get('link');
    if (!link) return;
    const provider = searchParams.get('provider') ?? undefined;
    const code = searchParams.get('code') ?? undefined;
    setLinkBanner({
      kind: link === 'success' ? 'success' : 'failed',
      provider,
      code
    });
    const next = new URLSearchParams(searchParams);
    next.delete('link');
    next.delete('provider');
    next.delete('code');
    setSearchParams(next, { replace: true });
    if (link === 'success') {
      refetch();
    }
  }, [searchParams, setSearchParams, refetch]);

  // All hooks must run before the early return — keep them above the
  // isLoading branch so React's hook-order invariant holds across the
  // loading→loaded transition. (`providers` is derived from `data` on
  // every render so the useMemo dep is still stable.)
  const providers = data?.oauthProviders ?? [];
  const availableProviders = useMemo(() => {
    const linked = new Set(providers.map(p => p.provider));
    return ALL_PROVIDERS.filter(p => !linked.has(p));
  }, [providers]);

  const onlyCredential =
    !data?.passwordUsableForLogin && providers.length === 1;
  // The last-credential warning has two remedies depending on WHY the
  // password can't back up this provider: a set-but-disabled password
  // needs the surface's method re-enabled (setting one is impossible —
  // one already exists), while no hash at all means "set a password".
  const passwordSetButDisabled =
    !!data?.hasPasswordSet && !data?.passwordUsableForLogin;

  const onStartLink = async (provider: OAuthProvider) => {
    setError(null);
    setLinkBanner(null);
    try {
      const res = await initiateLink({ provider }).unwrap();
      // Hand off to the IdP. The shared callback redirects back to
      // /user/security?tab=oauth&link=success|failed&provider=<x> so
      // the useEffect above renders the outcome banner.
      window.location.assign(res.authUrl);
    } catch (err: unknown) {
      const e = err as {
        data?: { detail?: string; title?: string; code?: string };
      };
      if (e?.data?.code === 'step_up_required') return; // StepUpModal handles
      if (e?.data?.code === 'password_confirm_required') return; // PasswordConfirmModal handles
      if (e?.data?.code === 'mfa_enrollment_required') {
        setError(t('userSecurity.linkedProvidersTab.errorMfaRequiredLink'));
        return;
      }
      setError(
        e?.data?.detail ||
          e?.data?.title ||
          t('userSecurity.linkedProvidersTab.errorStartFlow')
      );
    }
  };

  const onConfirmUnlink = async () => {
    if (!target) return;
    setError(null);
    try {
      await unlink({ provider: target.provider }).unwrap();
      setTarget(null);
    } catch (err: unknown) {
      const e = err as {
        data?: { detail?: string; title?: string; code?: string };
      };
      const code = e?.data?.code;
      if (code === 'last_credential') {
        setError(t('userSecurity.linkedProvidersTab.errorLastCredential'));
      } else if (
        code === 'step_up_required' ||
        code === 'password_confirm_required'
      ) {
        // The global StepUpModal / PasswordConfirmModal will pick this
        // up and replay; close the inline modal so the prompt isn't
        // obscured.
        setTarget(null);
      } else if (code === 'mfa_enrollment_required') {
        setTarget(null);
        setError(t('userSecurity.linkedProvidersTab.errorMfaRequiredUnlink'));
      } else {
        setError(
          e?.data?.detail ||
            e?.data?.title ||
            t('userSecurity.linkedProvidersTab.errorUnlinkGeneric')
        );
      }
    }
  };

  const columns: ColumnDef<SelfAuthOAuthProvider>[] = [
    {
      accessorKey: 'provider',
      header: t('userSecurity.linkedProvidersTab.colProvider'),
      cell: ({
        row: { original }
      }: CellContext<SelfAuthOAuthProvider, unknown>) => (
        <>
          <FontAwesomeIcon
            icon={PROVIDER_ICONS[original.provider]}
            className="me-2 text-700"
          />
          <span className="fw-semibold text-900">
            {PROVIDER_LABELS[original.provider]}
          </span>
          {original.isPrimary && (
            <SubtleBadge bg="primary" pill className="ms-2 fs-11 fw-normal">
              {t('userSecurity.linkedProvidersTab.primaryBadge')}
            </SubtleBadge>
          )}
        </>
      )
    },
    {
      accessorKey: 'email',
      header: t('userSecurity.linkedProvidersTab.colEmail'),
      cell: ({
        row: { original }
      }: CellContext<SelfAuthOAuthProvider, unknown>) => (
        <span className="text-700">{original.email}</span>
      )
    },
    {
      id: 'linkedAt',
      // Formatted accessor + timestamp comparator — see byTimestamp. This
      // table is compact (no search box) today; keeping the idiom uniform is
      // what stops flipping that flag from reintroducing the bug.
      accessorFn: p => formatDate(p.linkedAt),
      sortingFn: byTimestamp<SelfAuthOAuthProvider>(p => p.linkedAt),
      header: t('userSecurity.linkedProvidersTab.colLinked'),
      cell: ({
        row: { original }
      }: CellContext<SelfAuthOAuthProvider, unknown>) => (
        <span className="text-700">{formatDate(original.linkedAt)}</span>
      )
    },
    {
      id: 'actions',
      header: t('userSecurity.linkedProvidersTab.colActions'),
      enableSorting: false,
      meta: {
        headerProps: { className: 'text-end' },
        cellProps: { className: 'text-end' }
      },
      cell: ({
        row: { original }
      }: CellContext<SelfAuthOAuthProvider, unknown>) => (
        <IconButton
          size="sm"
          variant="outline-secondary"
          icon={faLinkSlash}
          disabled={onlyCredential || isFetching}
          onClick={() => setTarget(original)}
        >
          {t('userSecurity.linkedProvidersTab.rowUnlink')}
        </IconButton>
      )
    }
  ];

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  return (
    <>
      <div className="d-flex justify-content-between align-items-start flex-wrap gap-2 mb-3">
        <p className="fs-10 text-muted mb-0">
          {t('userSecurity.linkedProvidersTab.intro')}
        </p>
        {availableProviders.length > 0 && (
          <Dropdown as={ButtonGroup}>
            <Dropdown.Toggle
              variant="outline-primary"
              size="sm"
              className="text-nowrap"
              disabled={linkPending}
            >
              <FontAwesomeIcon icon={faLink} className="me-1" />
              {linkPending
                ? t('userSecurity.linkedProvidersTab.linkButtonStarting')
                : t('userSecurity.linkedProvidersTab.linkButton')}
            </Dropdown.Toggle>
            <Dropdown.Menu align="end">
              {availableProviders.map(p => (
                <Dropdown.Item key={p} onClick={() => onStartLink(p)}>
                  <FontAwesomeIcon
                    icon={PROVIDER_ICONS[p]}
                    className="me-2 text-700"
                  />
                  {PROVIDER_LABELS[p]}
                </Dropdown.Item>
              ))}
            </Dropdown.Menu>
          </Dropdown>
        )}
      </div>

      {linkBanner?.kind === 'success' && (
        <Alert
          variant="success"
          dismissible
          onClose={() => setLinkBanner(null)}
          className="fs-10"
        >
          {linkBanner.provider
            ? t('userSecurity.linkedProvidersTab.bannerSuccessProvider', {
                provider:
                  PROVIDER_LABELS[linkBanner.provider as OAuthProvider] ??
                  linkBanner.provider
              })
            : t('userSecurity.linkedProvidersTab.bannerSuccessGeneric')}
        </Alert>
      )}
      {linkBanner?.kind === 'failed' && (
        <Alert
          variant="danger"
          dismissible
          onClose={() => setLinkBanner(null)}
          className="fs-10"
        >
          {isKnownFailure(linkBanner.code)
            ? t(
                `userSecurity.linkedProvidersTab.linkFailures.${linkBanner.code}`
              )
            : t('userSecurity.linkedProvidersTab.bannerFailureGeneric')}
        </Alert>
      )}
      {error && (
        <Alert variant="danger" className="fs-10">
          {error}
        </Alert>
      )}

      {providers.length === 0 ? (
        <SecurityEmptyState
          icon={faLink}
          message={t('userSecurity.linkedProvidersTab.emptyNoLinked')}
          hint={
            availableProviders.length > 0
              ? t('userSecurity.linkedProvidersTab.emptyHasMore')
              : t('userSecurity.linkedProvidersTab.emptyAllLinked')
          }
        />
      ) : (
        <>
          {onlyCredential && (
            <Alert variant="warning" className="fs-10">
              {passwordSetButDisabled
                ? t(
                    'userSecurity.linkedProvidersTab.onlyCredentialWarningPasswordDisabled'
                  )
                : t('userSecurity.linkedProvidersTab.onlyCredentialWarning')}
            </Alert>
          )}
          <SecurityTable data={providers} columns={columns} compact />
        </>
      )}

      <Modal show={!!target} onHide={() => setTarget(null)} centered>
        <Modal.Header closeButton>
          <Modal.Title>
            {t('userSecurity.linkedProvidersTab.modalTitle', {
              provider: target ? PROVIDER_LABELS[target.provider] : ''
            })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && (
            <Alert variant="danger" className="fs-10">
              {error}
            </Alert>
          )}
          <p className="mb-0">
            {t('userSecurity.linkedProvidersTab.modalBody', {
              provider: target ? PROVIDER_LABELS[target.provider] : ''
            })}
          </p>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setTarget(null)}>
            {t('userSecurity.linkedProvidersTab.modalCancel')}
          </Button>
          <Button
            variant="danger"
            onClick={onConfirmUnlink}
            disabled={unlinkPending}
          >
            {unlinkPending
              ? t('userSecurity.linkedProvidersTab.modalSubmitting')
              : t('userSecurity.linkedProvidersTab.modalSubmit')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default LinkedProvidersTab;
