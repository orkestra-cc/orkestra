import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import { faFileInvoiceDollar, faBuilding, faTruck, faBell } from '@fortawesome/free-solid-svg-icons';
import FalconCardHeader from 'components/common/FalconCardHeader';
import Flex from 'components/common/Flex';

const BillingGreetings = () => {
  const quickLinks = [
    {
      title: 'Nuova Fattura',
      icon: faFileInvoiceDollar,
      to: '/billing/invoices/issued/new',
      color: 'primary',
    },
    {
      title: 'Clienti',
      icon: faBuilding,
      to: '/admin/clients',
      color: 'info',
    },
    {
      title: 'Fornitori',
      icon: faTruck,
      to: '/billing/suppliers',
      color: 'success',
    },
    {
      title: 'Notifiche SDI',
      icon: faBell,
      to: '/billing/notifications',
      color: 'warning',
    },
  ];

  return (
    <Card>
      <FalconCardHeader
        title="Fatturazione Elettronica"
        titleTag="h5"
        className="py-2"
        light
      />
      <Card.Body className="py-3">
        <Row className="g-3">
          {quickLinks.map((link) => (
            <Col key={link.title} sm={6} lg={3}>
              <Link
                to={link.to}
                className="text-decoration-none"
              >
                <Flex
                  alignItems="center"
                  className={`p-3 rounded bg-${link.color}-subtle text-${link.color} hover-shadow transition-all`}
                >
                  <FontAwesomeIcon icon={link.icon} className="fs-4 me-3" />
                  <span className="fw-medium">{link.title}</span>
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
