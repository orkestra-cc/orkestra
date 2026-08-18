import { Link } from 'react-router';
import { Col, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import Logo from 'components/common/Logo';
import Section from 'components/common/Section';

interface AuthCardLayoutProps {
  children: React.ReactNode;
  footer?: boolean;
}

/**
 * Shell for every auth surface (login, register, MFA, password flows): the
 * system canvas carrying one white card — no gradient panel, no decorative
 * shapes. Each page provides its own <Card>; this layout owns the brand mark
 * above it and the access/terms notices below it.
 */
const AuthCardLayout: React.FC<AuthCardLayoutProps> = ({
  children,
  footer = true
}) => {
  const { t } = useTranslation();
  return (
    <Section fluid className="py-0">
      <Row className="g-0 min-vh-100 flex-center">
        <Col xs={11} sm={8} md={6} lg={5} xl={4} xxl={3} className="py-5">
          <div className="text-center mb-4">
            <Logo width={140} className="mb-0" />
          </div>
          {children}
          <p className="text-center text-600 fs-10 mt-4 mb-1">
            {t('auth.cardLayout.restrictedNotice')}
          </p>
          <p className="text-center text-600 fs-10 mb-0">
            {t('auth.cardLayout.unauthorizedNotice')}
          </p>
          {footer && (
            <p className="text-center fs-10 mt-3 mb-0">
              {t('auth.cardLayout.termsPrefix')}{' '}
              <Link to="#!">{t('auth.cardLayout.termsLink')}</Link>{' '}
              {t('auth.cardLayout.termsConjunction')}{' '}
              <Link to="#!">{t('auth.cardLayout.conditionsLink')}</Link>
            </p>
          )}
        </Col>
      </Row>
    </Section>
  );
};

export default AuthCardLayout;
