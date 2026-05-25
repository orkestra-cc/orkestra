import { useState } from 'react';
import { Button, Form, Alert, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faGoogle,
  faApple,
  faGithub,
  faDiscord
} from '@fortawesome/free-brands-svg-icons';
import { IconDefinition } from '@fortawesome/fontawesome-svg-core';
import { useTranslation } from 'react-i18next';
import { initiateSocialLogin, SocialProvider } from 'utils/socialAuthUtils';
import { useGetOAuthProvidersQuery } from 'store/api/authApi';

import runtimeConfig from 'config/environment';

interface SocialLoginFormProps {
  backendUrl?: string;
  onError?: (error: Error) => void;
}

// PROVIDER_META is the FE-side mapping from a backend provider string to
// its visual identity (icon, button label, Bootstrap variant). Keep it
// in sync with the OAuthProvider enum on the backend
// (backend/internal/core/auth/models). A provider string the backend
// returns that's missing from this map is skipped at render time + logged
// to the console so the gap is visible to operators.
const PROVIDER_META: Record<
  SocialProvider,
  { icon: IconDefinition; label: string; variant: string }
> = {
  google: { icon: faGoogle, label: 'Google', variant: 'success' },
  apple: { icon: faApple, label: 'Apple', variant: 'danger' },
  github: { icon: faGithub, label: 'GitHub', variant: 'secondary' },
  discord: { icon: faDiscord, label: 'Discord', variant: 'primary' }
};

const KNOWN_PROVIDERS = new Set(Object.keys(PROVIDER_META));

const SocialLoginForm = ({
  backendUrl = runtimeConfig.apiUrl,
  onError
}: SocialLoginFormProps) => {
  const { t } = useTranslation();
  const [loadingProvider, setLoadingProvider] = useState<SocialProvider | null>(
    null
  );
  const [error, setError] = useState<string>('');

  // Live list of providers the backend currently exposes for this audience.
  // Filtered server-side by (a) configured client_id presence and (b) the
  // OAuth Providers toggle tab on /admin/modules/auth. The hook returns
  // an empty list on network failure (fail-closed) — see authApi.
  const { data, isLoading, isError } = useGetOAuthProvidersQuery();

  // Filter the backend list against the FE icon/label map. Unknown
  // strings are skipped with a console warning so an operator can see
  // when a backend update needs a matching FE entry.
  const socialProviders = (data?.providers ?? [])
    .filter((name): name is SocialProvider => {
      if (KNOWN_PROVIDERS.has(name)) return true;

      console.warn(
        `SocialLoginForm: backend advertised unknown provider "${name}" — add it to PROVIDER_META`
      );
      return false;
    })
    .map(name => ({ provider: name, ...PROVIDER_META[name] }));

  const providerLabel = (provider: SocialProvider): string =>
    PROVIDER_META[provider]?.label ?? provider;

  const handleSocialLogin = async (provider: SocialProvider) => {
    setLoadingProvider(provider);
    setError('');

    try {
      await initiateSocialLogin(provider, backendUrl);
    } catch (err) {
      const errorMessage =
        err instanceof Error
          ? err.message
          : t('auth.social.initiateFailed', {
              provider: providerLabel(provider)
            });
      setError(errorMessage);
      if (onError) onError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoadingProvider(null);
    }
  };

  // Loading: placeholder while the providers query is in flight. We
  // intentionally do NOT render the buttons optimistically — the whole
  // point of this component is to honor the admin's enable/disable
  // toggles, so showing all four during the ~100ms fetch window would
  // briefly contradict that.
  if (isLoading) {
    return (
      <div className="text-center my-3" aria-busy="true">
        <Spinner animation="border" size="sm" className="me-2" />
        <small className="text-muted">{t('auth.social.loading')}</small>
      </div>
    );
  }

  // Error: the endpoint failed. Show an alert and no buttons. The
  // backend re-validates on every OAuth start anyway, so a degraded
  // FE doesn't open a security hole — at worst it locks the user out
  // of the social path until the next refresh.
  if (isError) {
    return (
      <Alert variant="warning" className="mb-3">
        {t('auth.social.loadError')}
      </Alert>
    );
  }

  // Empty: configured providers list is empty (either nothing was set
  // up in /admin/modules/auth or the admin disabled all of them).
  // Render a muted line so the user knows the section is intentionally
  // empty rather than broken.
  if (socialProviders.length === 0) {
    return (
      <div className="text-center my-3">
        <small className="text-muted">{t('auth.social.noneAvailable')}</small>
      </div>
    );
  }

  return (
    <Form>
      {error && (
        <Alert variant="danger" className="mb-3">
          <div className="d-flex justify-content-between align-items-center">
            <span>{error}</span>
            <Button
              variant="link"
              size="sm"
              className="p-0"
              onClick={() => setError('')}
            >
              ×
            </Button>
          </div>
        </Alert>
      )}

      <div className="d-grid gap-3">
        {socialProviders.map(({ provider, icon, label, variant }) => (
          <Button
            key={provider}
            onClick={() => handleSocialLogin(provider)}
            disabled={loadingProvider !== null}
            variant={variant}
            size="lg"
          >
            <FontAwesomeIcon icon={icon} className="me-2" />
            {loadingProvider === provider
              ? t('auth.social.redirectingTo', { provider: label })
              : label}
          </Button>
        ))}
      </div>

      <div className="text-center mt-4">
        <small className="text-muted">{t('auth.social.acceptingTerms')}</small>
      </div>
    </Form>
  );
};

export default SocialLoginForm;
