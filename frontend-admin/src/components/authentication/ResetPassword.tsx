import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import AuthCardLayout from 'layouts/AuthCardLayout';
import ResetPasswordForm from 'components/authentication/ResetPasswordForm';

const ResetPassword = () => {
  const { t } = useTranslation();
  return (
    <AuthCardLayout>
      <Card>
        <Card.Body className="p-4 p-sm-5">
          <div className="text-center mb-4">
            <h3 className="mb-2">{t('auth.pages.resetTitle')}</h3>
          </div>
          <ResetPasswordForm />
        </Card.Body>
      </Card>
    </AuthCardLayout>
  );
};

export default ResetPassword;
