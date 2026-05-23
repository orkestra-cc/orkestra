import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import {
  faFileInvoiceDollar,
  faBuilding,
  faTruck,
  faBell
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';

const BillingGreetings = () => {
  const { t } = useTranslation();
  const quickLinks = [
    {
      titleKey: 'billing.greetings.quickLinks.newInvoice',
      icon: faFileInvoiceDollar,
      to: '/billing/invoices/issued/new',
      color: 'primary'
    },
    {
      titleKey: 'billing.greetings.quickLinks.clients',
      icon: faBuilding,
      to: '/admin/clients',
      color: 'info'
    },
    {
      titleKey: 'billing.greetings.quickLinks.suppliers',
      icon: faTruck,
      to: '/billing/suppliers',
      color: 'success'
    },
    {
      titleKey: 'billing.greetings.quickLinks.sdiNotifications',
      icon: faBell,
      to: '/billing/notifications',
      color: 'warning'
    }
  ];

  return (
    <Card>
      <OrkestraCardHeader
        title={t('billing.greetings.cardTitle')}
        titleTag="h5"
        className="py-2"
        light
      />
      <Card.Body className="py-3">
        <Row className="g-3">
          {quickLinks.map(link => (
            <Col key={link.titleKey} sm={6} lg={3}>
              <Link to={link.to} className="text-decoration-none">
                <Flex
                  alignItems="center"
                  className={`p-3 rounded bg-${link.color}-subtle text-${link.color} hover-shadow transition-all`}
                >
                  <FontAwesomeIcon icon={link.icon} className="fs-4 me-3" />
                  <span className="fw-medium">{t(link.titleKey)}</span>
                </Flex>
              </Link>
            </Col>
          ))}
        </Row>
      </Card.Body>
    </Card>
  );
};

export default BillingGreetings;
