import { Card } from 'react-bootstrap';
import AuthCardLayout from 'layouts/AuthCardLayout';
import RegisterForm from 'components/authentication/RegisterForm';

const Register = () => (
  <AuthCardLayout>
    <Card>
      <Card.Body className="p-4 p-sm-5">
        <div className="text-center mb-4">
          <h3 className="mb-2">Create account</h3>
          <p className="text-muted mb-0">Get started with Orkestra in a minute.</p>
        </div>
        <RegisterForm />
      </Card.Body>
    </Card>
  </AuthCardLayout>
);

export default Register;
