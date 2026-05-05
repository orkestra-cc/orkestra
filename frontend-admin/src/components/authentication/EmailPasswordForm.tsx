import { useState, FormEvent } from 'react';
import { Alert, Button, Form } from 'react-bootstrap';
import { Link, useNavigate } from 'react-router-dom';
import { useAppDispatch } from 'store/hooks';
import { useLoginMutation } from 'store/api/authApi';
import { login as loginAction } from 'store/slices/authSlice';

const EmailPasswordForm = () => {
  const navigate = useNavigate();
  const dispatch = useAppDispatch();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [localError, setLocalError] = useState<string | null>(null);
  const [login, { isLoading }] = useLoginMutation();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setLocalError(null);

    if (!email || !password) {
      setLocalError('Please enter both email and password.');
      return;
    }

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
          },
        });
        return;
      }

      if (!result.user) {
        setLocalError('Unable to sign in. Please try again.');
        return;
      }
      dispatch(loginAction({ userData: result.user }));

      navigate('/dashboard/analytics');
    } catch (err: unknown) {
      const anyErr = err as { data?: { detail?: string }; status?: number };
      if (anyErr?.status === 401) {
        setLocalError('Invalid email or password.');
      } else if (anyErr?.status === 403) {
        setLocalError(anyErr?.data?.detail || 'Your email has not been verified.');
      } else if (anyErr?.status === 429) {
        setLocalError('Too many failed attempts. Please try again later.');
      } else {
        setLocalError(anyErr?.data?.detail || 'Unable to sign in. Please try again.');
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit}>
      {localError && (
        <Alert variant="danger" className="mb-3" onClose={() => setLocalError(null)} dismissible>
          {localError}
        </Alert>
      )}

      <Form.Group className="mb-3" controlId="login-email">
        <Form.Label>Email</Form.Label>
        <Form.Control
          type="email"
          name="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3" controlId="login-password">
        <div className="d-flex justify-content-between">
          <Form.Label>Password</Form.Label>
          <Link to="/forgot-password" className="fs--1">
            Forgot password?
          </Link>
        </div>
        <Form.Control
          type="password"
          name="password"
          placeholder="••••••••"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
      </Form.Group>

      <div className="d-grid mb-3">
        <Button type="submit" variant="primary" size="lg" disabled={isLoading}>
          {isLoading ? 'Signing in…' : 'Sign in'}
        </Button>
      </div>

      <div className="text-center">
        <small className="text-muted">
          Don&apos;t have an account? <Link to="/register">Create one</Link>
        </small>
      </div>
    </Form>
  );
};

export default EmailPasswordForm;
