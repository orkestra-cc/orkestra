import { useMemo, useState } from 'react';
import { Alert, Button, Card, Form } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import AuthCardLayout from 'layouts/AuthCardLayout';
import { useAppDispatch } from 'store/hooks';
import {
  useLoginVerifyMfaMutation,
  useWebAuthnLoginBeginMutation,
  useWebAuthnLoginFinishMutation
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeRequestOptions,
  encodeAssertion
} from 'store/api/webauthnCodec';
import { login as loginAction } from 'store/slices/authSlice';

export interface MfaVerifyPanelProps {
  /**
   * One-shot login challenge id. The caller holds it in component memory
   * (OAuth path) or in location.state (password path) — never in a URL.
   */
  challengeId: string;
  email?: string;
  webauthnAvailable: boolean;
  /** Already sanitised by the caller (sanitizeReturnTo ?? DEFAULT_POST_LOGIN). */
  returnTo: string;
}

interface MfaCodeForm {
  code: string;
}

/**
 * Completes a login that paused on the MFA challenge. Shared by the
 * password path (LoginMfaVerify page) and the OAuth callback
 * (SocialAuthCallback renders it locally from the scrubbed fragment). Either:
 *   - POST a TOTP / backup code to /v1/auth/operator/mfa/login/verify, or
 *   - run the WebAuthn assertion ceremony when webauthnAvailable and the
 *     user picks "Use a passkey".
 *
 * Both branches dispatch loginAction with the same BackendUser shape so
 * downstream consumers don't care which factor satisfied the partial.
 */
const MfaVerifyPanel = ({
  challengeId,
  email,
  webauthnAvailable,
  returnTo
}: MfaVerifyPanelProps) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();
  const passkeyOffered = webauthnAvailable && browserSupportsWebAuthn();

  const [useBackup, setUseBackup] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);
  const [passkeyBusy, setPasskeyBusy] = useState(false);

  const schema = useMemo(
    () =>
      yup.object({
        code: yup.string().trim().required(t('auth.mfa.errors.missingCode'))
      }),
    [t]
  );
  const {
    register,
    handleSubmit,
    resetField,
    formState: { errors }
  } = useForm<MfaCodeForm>({
    resolver: yupResolver(schema),
    defaultValues: { code: '' }
  });

  const [verify, { isLoading }] = useLoginVerifyMfaMutation();
  const [waBegin] = useWebAuthnLoginBeginMutation();
  const [waFinish] = useWebAuthnLoginFinishMutation();

  const onSubmit = async ({ code }: MfaCodeForm) => {
    setServerError(null);
    try {
      const res = await verify({
        challengeId,
        code: code.trim(),
        useBackup
      }).unwrap();
      dispatch(loginAction({ userData: res.user }));
      navigate(returnTo, { replace: true });
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 401) {
        setServerError(t('auth.mfa.errors.incorrectCode'));
      } else if (anyErr?.status === 429) {
        setServerError(t('auth.mfa.errors.tooMany'));
      } else {
        setServerError(anyErr?.data?.detail ?? t('auth.mfa.errors.generic'));
      }
    }
  };

  const handlePasskey = async () => {
    setServerError(null);
    setPasskeyBusy(true);
    try {
      const beginRes = await waBegin({
        loginChallengeId: challengeId
      }).unwrap();
      const opts = decodeRequestOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.get({
        publicKey: opts
      })) as PublicKeyCredential | null;
      if (!cred) {
        setPasskeyBusy(false);
        return;
      }
      const finishRes = await waFinish({
        loginChallengeId: challengeId,
        webauthnChallengeId: beginRes.challengeId,
        assertionResponse: encodeAssertion(cred)
      }).unwrap();
      dispatch(loginAction({ userData: finishRes.user }));
      navigate(returnTo, { replace: true });
    } catch (err: unknown) {
      const anyErr = err as {
        name?: string;
        status?: number;
        data?: { detail?: string };
      };
      if (anyErr?.name === 'NotAllowedError') {
        setServerError(t('auth.mfa.errors.passkeyCancelled'));
      } else if (anyErr?.status === 401) {
        setServerError(t('auth.mfa.errors.passkeyFailed'));
      } else {
        setServerError(
          anyErr?.data?.detail ?? t('auth.mfa.errors.passkeyGeneric')
        );
      }
      setPasskeyBusy(false);
    }
  };

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.mfa.title')}</h3>
            <p className="text-muted mb-0">
              {email
                ? t('auth.mfa.promptForEmail', { email })
                : t('auth.mfa.promptDefault')}
            </p>
          </div>

          {serverError && (
            <Alert
              variant="danger"
              className="mb-3"
              onClose={() => setServerError(null)}
              dismissible
            >
              {serverError}
            </Alert>
          )}

          {passkeyOffered && (
            <div className="d-grid mb-3">
              <Button
                variant="orkestra-default"
                size="lg"
                disabled={passkeyBusy}
                onClick={handlePasskey}
              >
                {passkeyBusy
                  ? t('auth.mfa.passkeyWaiting')
                  : t('auth.mfa.passkeyButton')}
              </Button>
              <div className="text-center text-muted fs-10 mt-2">
                {t('auth.mfa.passkeyOr')}
              </div>
            </div>
          )}

          <Form onSubmit={handleSubmit(onSubmit)} noValidate>
            <Form.Group className="mb-3">
              <Form.Label>
                {useBackup
                  ? t('auth.mfa.backupCode')
                  : t('auth.mfa.authenticatorCode')}
              </Form.Label>
              <Form.Control
                type="text"
                inputMode={useBackup ? 'text' : 'numeric'}
                autoComplete="one-time-code"
                autoFocus
                isInvalid={!!errors.code}
                placeholder={
                  useBackup
                    ? t('auth.mfa.backupPlaceholder')
                    : t('auth.mfa.authenticatorPlaceholder')
                }
                {...register('code')}
              />
              <Form.Control.Feedback type="invalid">
                {errors.code?.message}
              </Form.Control.Feedback>
            </Form.Group>

            <div className="d-grid mb-3">
              <Button
                type="submit"
                variant="orkestra-primary"
                size="lg"
                disabled={isLoading}
              >
                {isLoading ? t('auth.mfa.submitting') : t('auth.mfa.submit')}
              </Button>
            </div>

            <div className="d-flex justify-content-between fs-10">
              <button
                type="button"
                className="btn btn-link p-0"
                onClick={() => {
                  setUseBackup(v => !v);
                  resetField('code');
                }}
              >
                {useBackup
                  ? t('auth.mfa.useAuthenticator')
                  : t('auth.mfa.useBackup')}
              </button>
              <Link to="/login">{t('auth.mfa.back')}</Link>
            </div>
          </Form>
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default MfaVerifyPanel;
