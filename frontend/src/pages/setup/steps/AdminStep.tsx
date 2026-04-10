import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { useAppDispatch } from 'store/hooks';
import { login as loginAction, setAccessToken } from 'store/slices/authSlice';
import { useCreateInitialAdminMutation } from 'store/api/setupApi';

interface AdminStepProps {
  onNext: () => void;
}

/**
 * Second step of the setup wizard: collects the first administrator's name,
 * email and password, then calls POST /v1/setup/admin. On success the
 * returned access token and user are written to the auth slice so the
 * remaining steps run authenticated as the freshly-created developer user.
 */
const AdminStep = ({ onNext }: AdminStepProps) => {
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
      setError('All fields are required.');
      return;
    }
    if (password.length < 10) {
      setError('Password must be at least 10 characters.');
      return;
    }
    if (password !== confirmPassword) {
      setError('Passwords do not match.');
      return;
    }

    try {
      const result = await createAdmin({ email: email.trim(), password, fullName: fullName.trim() }).unwrap();

      // Mirror the normal login flow: stash user in the auth slice and the
      // access token in both localStorage (for legacy reads) and Redux (used
      // by baseApi's prepareHeaders to send the Bearer token on subsequent
      // requests).
      dispatch(loginAction({ userData: result.user }));
      dispatch(setAccessToken({ accessToken: result.accessToken, expiresIn: result.expiresIn }));
      if (result.accessToken) {
        localStorage.setItem('access_token', result.accessToken);
      }

      onNext();
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 409) {
        setError('An administrator already exists. Reload this page to continue to the login screen.');
      } else if (anyErr?.status === 400 && anyErr?.data?.detail) {
        setError(anyErr.data.detail);
      } else {
        setError(anyErr?.data?.detail || 'Could not create the administrator account. Please try again.');
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit} noValidate>
      <div className="mb-4">
        <h5 className="mb-1">Create the administrator account</h5>
        <p className="text-muted fs-10 mb-0">
          This user becomes the platform root with the <code>developer</code>{' '}
          role. You can add more users later from the admin UI.
        </p>
      </div>

      {error && (
        <Alert variant="danger" className="mb-3" onClose={() => setError(null)} dismissible>
          {error}
        </Alert>
      )}

      <Form.Group className="mb-3">
        <Form.Label>Full name</Form.Label>
        <Form.Control
          type="text"
          value={fullName}
          onChange={(e) => setFullName(e.target.value)}
          autoComplete="name"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>Email</Form.Label>
        <Form.Control
          type="email"
          placeholder="admin@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="email"
          required
        />
      </Form.Group>

      <Form.Group className="mb-3">
        <Form.Label>Password</Form.Label>
        <Form.Control
          type="password"
          placeholder="At least 10 characters"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          required
        />
        <Form.Text className="text-muted">
          Argon2id-hashed on the backend. Must be at least 10 characters and
          not contain your email address.
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-4">
        <Form.Label>Confirm password</Form.Label>
        <Form.Control
          type="password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          autoComplete="new-password"
          required
        />
      </Form.Group>

      <div className="d-flex justify-content-end">
        <Button type="submit" variant="primary" disabled={isLoading}>
          {isLoading ? (
            <>
              <Spinner animation="border" size="sm" className="me-2" />
              Creating...
            </>
          ) : (
            'Create administrator'
          )}
        </Button>
      </div>
    </Form>
  );
};

export default AdminStep;
