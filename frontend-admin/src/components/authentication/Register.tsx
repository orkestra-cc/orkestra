import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import RegisterForm from 'components/authentication/RegisterForm';

const Register = () => {
  const { t } = useTranslation();
  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.pages.registerTitle')}</h3>
            <p className="text-muted mb-0">
              {t('auth.pages.registerSubtitle')}
            </p>
          </div>
          <RegisterForm />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default Register;
