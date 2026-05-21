import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import {
  Alert,
  Badge,
  Button,
  ButtonGroup,
  Card,
  Dropdown,
  Modal,
  Spinner,
  Table
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useGetSelfAuthMethodsQuery,
  useInitiateOauthLinkSelfMutation,
  useUnlinkOauthSelfMutation,
  type OAuthProvider,
  type SelfAuthOAuthProvider
} from 'store/api/authApi';

// Provider brand names — proper nouns, intentionally not translated.
const PROVIDER_LABELS: Record<OAuthProvider, string> = {
  google: 'Google',
  apple: 'Apple',
  github: 'GitHub',
  discord: 'Discord'
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

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }

  const onlyCredential = !data?.hasUsablePassword && providers.length === 1;

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

  return (
    <>
      <Card className="shadow-none border">
        <Card.Header className="d-flex justify-content-between align-items-center flex-wrap gap-2">
          <Card.Title as="h5" className="mb-0">
            {t('userSecurity.linkedProvidersTab.title')}
          </Card.Title>
          {availableProviders.length > 0 && (
            <Dropdown as={ButtonGroup}>
              <Dropdown.Toggle
                variant="outline-primary"
                size="sm"
                disabled={linkPending}
              >
                {linkPending
                  ? t('userSecurity.linkedProvidersTab.linkButtonStarting')
                  : t('userSecurity.linkedProvidersTab.linkButton')}
              </Dropdown.Toggle>
              <Dropdown.Menu align="end">
                {availableProviders.map(p => (
                  <Dropdown.Item key={p} onClick={() => onStartLink(p)}>
                    {PROVIDER_LABELS[p]}
                  </Dropdown.Item>
                ))}
              </Dropdown.Menu>
            </Dropdown>
          )}
        </Card.Header>
        <Card.Body>
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
            <p className="fs-10 text-muted mb-0">
              {t('userSecurity.linkedProvidersTab.emptyNoLinked')}{' '}
              {availableProviders.length > 0
                ? t('userSecurity.linkedProvidersTab.emptyHasMore')
                : t('userSecurity.linkedProvidersTab.emptyAllLinked')}
            </p>
          ) : (
            <>
              {onlyCredential && (
                <Alert variant="warning" className="fs-10">
                  {t('userSecurity.linkedProvidersTab.onlyCredentialWarning')}
                </Alert>
              )}
              <Table responsive size="sm" className="mb-0 align-middle">
                <thead>
                  <tr>
                    <th>{t('userSecurity.linkedProvidersTab.colProvider')}</th>
                    <th>{t('userSecurity.linkedProvidersTab.colEmail')}</th>
                    <th>{t('userSecurity.linkedProvidersTab.colLinked')}</th>
                    <th className="text-end">
                      {t('userSecurity.linkedProvidersTab.colActions')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {providers.map(p => (
                    <tr key={p.provider}>
                      <td>
                        {PROVIDER_LABELS[p.provider]}
                        {p.isPrimary && (
                          <Badge bg="primary" className="ms-2">
                            {t('userSecurity.linkedProvidersTab.primaryBadge')}
                          </Badge>
                        )}
                      </td>
                      <td className="fs-10">{p.email}</td>
                      <td className="fs-10 text-muted">
                        {new Date(p.linkedAt).toLocaleDateString()}
                      </td>
                      <td className="text-end">
                        <Button
                          variant="outline-danger"
                          size="sm"
                          disabled={onlyCredential || isFetching}
                          onClick={() => setTarget(p)}
                        >
                          {t('userSecurity.linkedProvidersTab.rowUnlink')}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </>
          )}
        </Card.Body>
      </Card>

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
