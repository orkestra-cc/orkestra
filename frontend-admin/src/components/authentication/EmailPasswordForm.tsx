import { useState } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useAppDispatch } from 'store/hooks';
import {
  passwordUiVisible,
  useGetAuthPolicyQuery,
  useLoginMutation
} from 'store/api/authApi';
import { login as loginAction } from 'store/slices/authSlice';
import {
  DEFAULT_POST_LOGIN,
  locationToReturnTo,
  sanitizeReturnTo
} from 'utils/returnTo';

const schema = yup.object({
  email: yup.string().email().required(),
  password: yup.string().required()
});

type LoginFormData = yup.InferType<typeof schema>;

const EmailPasswordForm = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const dispatch = useAppDispatch();
  // Where ProtectedRoute wanted to send the user before bouncing them to login.
  // Sanitised against open-redirect / auth-loop targets; null falls back to the
  // dashboard. Survives the MFA hop by riding along in /mfa/verify's state.
  const returnTo = sanitizeReturnTo(
    locationToReturnTo((location.state as { from?: unknown } | null)?.from)
  );
  const [localError, setLocalError] = useState<string | null>(null);
  const [login, { isLoading }] = useLoginMutation();
  const {
    register,
    handleSubmit,
    formState: { errors }
  } = useForm<LoginFormData>({ resolver: yupResolver(schema) });
  // Surface admin-managed kill switches. The transport-failure fallback in
  // authApi keeps everything enabled so a degraded /policy fetch doesn't
  // block legitimate users; a SERVED false/null is honoured strictly.
  const { data: policy } = useGetAuthPolicyQuery();
  const loginEnabled = policy?.loginEnabled ?? true;
  const registrationEnabled = policy?.registrationEnabled ?? true;
  const breakGlass = policy?.passwordLoginBreakGlassEffective ?? false;
  const persistedOn = passwordUiVisible(policy);

  // G5: persisted false/null hides the password UI entirely — the backend
  // would 403 anyway; a sign-in page must not advertise a dead method.
  // The ONE exception is the labelled emergency form under break-glass.
  if (!persistedOn && !breakGlass) return null;

  // Under break-glass with the persisted method off, this is an emergency
  // surface: label it, and hide every credential-minting CTA.
  const emergencyOnly = breakGlass && !persistedOn;

  const onSubmit = handleSubmit(async ({ email, password }) => {
    setLocalError(null);
    try {
      const result = await login({ email, password }).unwrap();

      // Account has an enrolled second factor — hold the credentials flow
      // and send the user to the verify page with the challenge id.
      if (result.requiresMfa && result.mfaToken) {
        navigate('/mfa/verify', {
          state: {
            challengeId: result.mfaToken,
            email,
            webauthnAvailable: result.webauthnAvailable ?? false,
            returnTo
          }
        });
        return;
      }

      if (!result.user) {
        setLocalError(t('auth.errors.unableToSignIn'));
        return;
      }
      dispatch(loginAction({ userData: result.user }));

      navigate(returnTo ?? DEFAULT_POST_LOGIN, { replace: true });
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      if (anyErr?.status === 401) {
        setLocalError(t('auth.errors.invalidCredentials'));
      } else if (anyErr?.status === 403) {
        setLocalError(
          anyErr?.data?.detail || t('auth.errors.emailNotVerified')
        );
      } else if (anyErr?.status === 429) {
        setLocalError(t('auth.errors.tooManyAttempts'));
      } else {
        setLocalError(anyErr?.data?.detail || t('auth.errors.unableToSignIn'));
      }
    }
  });

  return (
    <Form onSubmit={onSubmit} noValidate>
      {emergencyOnly && (
        <Alert variant="warning" className="mb-3">
          {t('auth.pages.passwordBreakGlassActive')}
        </Alert>
      )}
      {!loginEnabled && (
        <Alert variant="warning" className="mb-3">
          {t('auth.loginDisabled')}
        </Alert>
      )}
      {localError && (
        <Alert
          variant="danger"
          className="mb-3"
          onClose={() => setLocalError(null)}
          dismissible
        >
          {localError}
        </Alert>
      )}

      <Form.Group className="mb-3" controlId="login-email">
        <Form.Label>{t('auth.email')}</Form.Label>
        <Form.Control
          type="email"
          placeholder={t('auth.emailPlaceholder')}
          autoComplete="email"
          isInvalid={!!errors.email}
          {...register('email')}
        />
        <Form.Control.Feedback type="invalid">
          {errors.email?.type === 'email'
            ? t('auth.errors.invalidEmail')
            : t('auth.errors.missingFields')}
        </Form.Control.Feedback>
      </Form.Group>

      <Form.Group className="mb-3" controlId="login-password">
        <div className="d-flex justify-content-between">
          <Form.Label>{t('auth.password')}</Form.Label>
          {!emergencyOnly && (
            <Link to="/forgot-password" className="fs-10">
              {t('auth.forgotPassword')}
            </Link>
          )}
        </div>
        <Form.Control
          type="password"
          placeholder={t('auth.passwordPlaceholder')}
          autoComplete="current-password"
          isInvalid={!!errors.password}
          {...register('password')}
        />
        <Form.Control.Feedback type="invalid">
          {t('auth.errors.missingFields')}
        </Form.Control.Feedback>
      </Form.Group>

      <div className="d-grid mb-3">
        <Button
          type="submit"
          variant="primary"
          size="lg"
          disabled={isLoading || !loginEnabled}
        >
          {isLoading ? t('auth.signingIn') : t('auth.signIn')}
        </Button>
      </div>

      {registrationEnabled && !emergencyOnly && (
        <div className="text-center">
          <small className="text-muted">
            {t('auth.noAccount')}{' '}
            <Link to="/register">{t('auth.createOne')}</Link>
          </small>
        </div>
      )}
    </Form>
  );
};

export default EmailPasswordForm;
