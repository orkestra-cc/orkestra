import { Card } from 'react-bootstrap';
import { useSearchParams } from 'react-router-dom';
import AuthCardLayout from 'layouts/AuthCardLayout';
import EmailPasswordForm from 'components/authentication/EmailPasswordForm';
import SocialLoginForm from 'components/authentication/SocialLoginForm';

const Login = () => {
  const [searchParams] = useSearchParams();
  const registered = searchParams.get('registered');
  const reset = searchParams.get('reset');

  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">Sign in</h3>
            <p className="text-muted mb-0">Welcome back to Orkestra.</p>
          </div>

          {registered && (
            <div className="alert alert-success">
              Account created. You can now sign in.
            </div>
          )}
          {reset && (
            <div className="alert alert-success">
              Password updated. Please sign in with your new password.
            </div>
          )}

          <EmailPasswordForm />

          <div className="position-relative mt-4 mb-3">
            <hr className="text-300" />
            <div className="divider-content-center">or continue with</div>
          </div>

          <SocialLoginForm />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default Login;
