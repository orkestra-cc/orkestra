import { Card } from 'react-bootstrap';
import SocialLoginForm from 'components/authentication/SocialLoginForm';
import AuthCardLayout from 'layouts/AuthCardLayout';

const Login = () => (
  <AuthCardLayout>
    <Card>
      <Card.Body className="p-4 p-sm-5">
        <div className="text-center mb-4">
          <h3 className="mb-3">Welcome</h3>
          <p className="text-muted">
            Sign in with your account to continue.
          </p>
        </div>
        <SocialLoginForm />
      </Card.Body>
    </Card>
  </AuthCardLayout>
);

export default Login;
