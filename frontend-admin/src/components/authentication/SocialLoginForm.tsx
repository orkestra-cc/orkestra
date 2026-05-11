import { useState } from 'react';
import { Button, Form, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faGoogle,
  faApple
} from '@fortawesome/free-brands-svg-icons';
import { initiateSocialLogin, SocialProvider } from 'utils/socialAuthUtils';

interface SocialLoginFormProps {
  backendUrl?: string;
  onError?: (error: Error) => void;
}

const SocialLoginForm = ({
  backendUrl = import.meta.env.VITE_BACKEND_URL,
  onError
}: SocialLoginFormProps) => {
  const [loadingProvider, setLoadingProvider] = useState<SocialProvider | null>(
    null
  );
  const [error, setError] = useState<string>('');

  const handleSocialLogin = async (provider: SocialProvider) => {
    setLoadingProvider(provider);
    setError('');

    try {
      await initiateSocialLogin(provider, backendUrl);
    } catch (err) {
      const errorMessage =
        err instanceof Error
          ? err.message
          : `Failed to initiate ${provider} login`;
      setError(errorMessage);
      if (onError) onError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoadingProvider(null);
    }
  };

  const socialProviders = [
    {
      provider: 'google' as SocialProvider,
      icon: faGoogle,
      label: 'Google',
      variant: 'success',
      loadingText: 'Redirecting to Google...'
    },
    {
      provider: 'apple' as SocialProvider,
      icon: faApple,
      label: 'Apple',
      variant: 'danger',
      loadingText: 'Redirecting to Apple...'
    }
    // {
    //   provider: 'github' as SocialProvider,
    //   icon: faGithub,
    //   label: 'GitHub',
    //   variant: 'secondary',
    //   loadingText: 'Redirecting to GitHub...'
    // },
    // {
    //   provider: 'discord' as SocialProvider,
    //   icon: faDiscord,
    //   label: 'Discord',
    //   variant: 'primary',
    //   loadingText: 'Redirecting to Discord...'
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
        {socialProviders.map(
          ({ provider, icon, label, variant, loadingText }) => (
            <Button
              key={provider}
              onClick={() => handleSocialLogin(provider)}
              disabled={loadingProvider !== null}
              variant={variant}
              size="lg"
            >
              <FontAwesomeIcon icon={icon} className="me-2" />
              {loadingProvider === provider ? loadingText : label}
            </Button>
          )
        )}
      </div>

      <div className="text-center mt-4">
        <small className="text-muted">
          Accedendo, accetti i nostri Termini di servizio e l’Informativa sulla
          privacy.
        </small>
      </div>
    </Form>
  );
};

export default SocialLoginForm;
