import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
import { useAppDispatch } from 'store/hooks';
import { login as loginAction, setAccessToken } from 'store/slices/authSlice';
import { useCreateInitialAdminMutation } from 'store/api/setupApi';

interface AdminStepProps {
  /**
   * Called once the admin is created and the auth slice is hydrated.
   * The fullName is propagated upward so the next step (organization)
   * can pre-fill a sensible default like "{first name}'s Workspace".
   */
  onNext: (fullName: string) => void;
}

/**
 * Second step of the setup wizard: collects the first administrator's name,
 * email and password, then calls POST /v1/setup/admin. On success the
 * returned access token and user are written to the auth slice so the
 * remaining steps run authenticated as the freshly-created developer user.
 */
const AdminStep = ({ onNext }: AdminStepProps) => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const [createAdmin, { isLoading }] = useCreateInitialAdminMutation();

  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!fullName.trim() || !email.trim() || !password || !confirmPassword) {
      setError(t('setup.admin.errorAllRequired'));
      return;
    }
    if (password.length < 10) {
      setError(t('setup.admin.errorPasswordLength'));
      return;
    }
    if (password !== confirmPassword) {
      setError(t('setup.admin.errorPasswordMismatch'));
      return;
    }

    try {
      const result = await createAdmin({
        email: email.trim(),
        password,
        fullName: fullName.trim()
      }).unwrap();

      dispatch(loginAction({ userData: result.user }));
      dispatch(
        setAccessToken({
          accessToken: result.accessToken,
          expiresIn: result.expiresIn
        })
      );

      onNext(fullName.trim());
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 409) {
        setError(t('setup.admin.errorConflict'));
      } else if (anyErr?.status === 400 && anyErr?.data?.detail) {
        setError(anyErr.data.detail);
      } else {
        setError(anyErr?.data?.detail || t('setup.admin.errorGeneric'));
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit} noValidate>
      <div className="mb-4">
        <h5 className="mb-1">{t('setup.admin.title')}</h5>
        <p className="text-muted fs-10 mb-0">
          <Trans i18nKey="setup.admin.intro" components={{ code: <code /> }} />
        </p>
      </div>

      {error && (
        <Alert
          variant="danger"
          className="mb-3"
          onClose={() => setError(null)}
          dismissible
        >
          {error}
        </Alert>
      )}

      <Form.Group className="mb-3">
        <Form.Label>{t('setup.admin.labelFullName')}</Form.Label>
        <Form.Control
          type="text"
          value={fullName}
          onChange={e => setFullName(e.target.value)}
          autoComplete="name"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>{t('setup.admin.labelEmail')}</Form.Label>
        <Form.Control
          type="email"
          placeholder={t('setup.admin.placeholderEmail')}
          value={email}
          onChange={e => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>{t('setup.admin.labelPassword')}</Form.Label>
        <Form.Control
          type="password"
          placeholder={t('setup.admin.placeholderPassword')}
          value={password}
          onChange={e => setPassword(e.target.value)}
          autoComplete="new-password"
          required
        />
        <Form.Text className="text-muted">
          {t('setup.admin.passwordHelp')}
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-4">
        <Form.Label>{t('setup.admin.labelConfirmPassword')}</Form.Label>
        <Form.Control
          type="password"
          value={confirmPassword}
          onChange={e => setConfirmPassword(e.target.value)}
          autoComplete="new-password"
          required
        />
      </Form.Group>

      <div className="d-flex justify-content-end">
        <Button type="submit" variant="primary" disabled={isLoading}>
          {isLoading ? (
            <>
              <Spinner animation="border" size="sm" className="me-2" />
              {t('setup.admin.submitting')}
            </>
          ) : (
            t('setup.admin.submit')
          )}
        </Button>
      </div>
    </Form>
  );
};

export default AdminStep;
