import { useEffect, useState } from 'react';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { Link, useSearchParams } from 'react-router-dom';
import AuthCardLayout from 'layouts/AuthCardLayout';
import {
  useVerifyEmailMutation,
  useResendVerificationMutation,
} from 'store/api/authApi';

type Status = 'pending' | 'verifying' | 'success' | 'error';

const VerifyEmailPage = () => {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token');
  const pendingEmail = searchParams.get('pending');
  const [status, setStatus] = useState<Status>(token ? 'verifying' : 'pending');
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [verifyEmail] = useVerifyEmailMutation();
  const [resendVerification, { isLoading: resending, isSuccess: resent }] =
    useResendVerificationMutation();

  useEffect(() => {
    const run = async () => {
      if (!token) return;
      try {
        await verifyEmail({ token }).unwrap();
        setStatus('success');
      } catch (err: unknown) {
        const anyErr = err as { data?: { detail?: string } };
        setErrorMessage(anyErr?.data?.detail || 'The verification link is invalid or has expired.');
        setStatus('error');
      }
    };
    void run();
  }, [token, verifyEmail]);

  const handleResend = async () => {
    if (!pendingEmail) return;
    try {
      await resendVerification({ email: pendingEmail }).unwrap();
    } catch {
      // Fall through — response is always generic.
    }
  };

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-3">Email verification</h3>
          </div>

          {status === 'pending' && pendingEmail && (
            <>
              <Alert variant="info">
                We&apos;ve sent a verification email to <strong>{pendingEmail}</strong>. Click the link
                in that email to activate your account.
              </Alert>
              <div className="text-center mt-3">
                <Button variant="outline-primary" onClick={handleResend} disabled={resending || resent}>
                  {resent ? 'Email sent' : resending ? 'Sending…' : 'Resend verification email'}
                </Button>
              </div>
            </>
          )}

          {status === 'pending' && !pendingEmail && (
            <Alert variant="warning">
              No verification token was provided. Check your email for a verification link.
            </Alert>
          )}

          {status === 'verifying' && (
            <div className="text-center">
              <Spinner animation="border" />
              <p className="mt-3 text-muted">Verifying your email…</p>
            </div>
          )}

          {status === 'success' && (
            <>
              <Alert variant="success">
                Your email has been verified. You can now sign in.
              </Alert>
              <div className="d-grid">
                <Link to="/login" className="btn btn-primary btn-lg">
                  Sign in
                </Link>
              </div>
            </>
          )}

          {status === 'error' && (
            <>
              <Alert variant="danger">{errorMessage}</Alert>
              <div className="text-center">
                <Link to="/login">Back to sign in</Link>
              </div>
            </>
          )}
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default VerifyEmailPage;
