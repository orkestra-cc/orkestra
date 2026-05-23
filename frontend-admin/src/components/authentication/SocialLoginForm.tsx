import { useState } from 'react';
import { Button, Form, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faGoogle, faApple } from '@fortawesome/free-brands-svg-icons';
import { useTranslation } from 'react-i18next';
import { initiateSocialLogin, SocialProvider } from 'utils/socialAuthUtils';

import runtimeConfig from 'config/environment';

interface SocialLoginFormProps {
  backendUrl?: string;
  onError?: (error: Error) => void;
}

const SocialLoginForm = ({
  backendUrl = runtimeConfig.apiUrl,
  onError
}: SocialLoginFormProps) => {
  const { t } = useTranslation();
  const [loadingProvider, setLoadingProvider] = useState<SocialProvider | null>(
    null
  );
  const [error, setError] = useState<string>('');

  const providerLabel = (provider: SocialProvider) =>
    provider === 'google'
      ? 'Google'
      : provider === 'apple'
        ? 'Apple'
        : provider;

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

  const socialProviders: Array<{
    provider: SocialProvider;
    icon: typeof faGoogle;
    label: string;
    variant: string;
  }> = [
    {
      provider: 'google',
      icon: faGoogle,
      label: 'Google',
      variant: 'success'
    },
    {
      provider: 'apple',
      icon: faApple,
      label: 'Apple',
      variant: 'danger'
    }
    // {
    //   provider: 'github' as SocialProvider,
    //   icon: faGithub,
    //   label: 'GitHub',
    //   variant: 'secondary'
    // },
    // {
    //   provider: 'discord' as SocialProvider,
    //   icon: faDiscord,
    //   label: 'Discord',
    //   variant: 'primary'
    // }
  ];

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
