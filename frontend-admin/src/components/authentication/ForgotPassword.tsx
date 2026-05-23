import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import ForgotPasswordForm from 'components/authentication/ForgotPasswordForm';

const ForgotPassword = () => {
  const { t } = useTranslation();
  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.pages.forgotTitle')}</h3>
          </div>
          <ForgotPasswordForm />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default ForgotPassword;
