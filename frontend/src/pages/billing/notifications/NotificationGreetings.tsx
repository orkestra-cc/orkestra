import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faBell,
  faCheck,
  faTimes,
  faClock,
  faArrowLeft,
} from '@fortawesome/free-solid-svg-icons';
import { Link } from 'react-router';
import FalconCardHeader from 'components/common/FalconCardHeader';
import Flex from 'components/common/Flex';
import { useGetNotificationSummaryQuery } from 'store/api/billingApi';
import CountUp from 'react-countup';

const NotificationGreetings = () => {
  const { data: summary } = useGetNotificationSummaryQuery();

  const statItems = [
    {
      title: 'Totale',
      value: summary?.total || 0,
      color: 'primary',
      icon: faBell,
    },
    {
      title: 'Da Processare',
      value: summary?.unprocessed || 0,
      color: 'warning',
      icon: faClock,
    },
    {
      title: 'Positive (RC)',
      value: summary?.RC || 0,
      color: 'success',
      icon: faCheck,
    },
    {
      title: 'Negative (NS)',
      value: summary?.NS || 0,
      color: 'danger',
      icon: faTimes,
    },
  ];

  return (
    <Card>
      <FalconCardHeader
        title={
          <Flex alignItems="center">
            <Link to="/billing/dashboard" className="text-body-tertiary me-2">
              <FontAwesomeIcon icon={faArrowLeft} />
            </Link>
            Notifiche SDI
          </Flex>
        }
        titleTag="h5"
        className="py-2"
        light
        endEl={
          summary && summary.unprocessed > 0 && (
            <span className="badge bg-warning rounded-pill">
              {summary.unprocessed} da gestire
            </span>
          )
        }
      />
      <Card.Body className="py-3">
        <Row className="g-3">
          {statItems.map((item) => (
            <Col key={item.title} sm={6} lg={3}>
              <Flex
                alignItems="center"
                className={`p-3 rounded bg-${item.color}-subtle`}
              >
                <div
                  className={`d-flex align-items-center justify-content-center rounded-circle bg-${item.color} text-white me-3`}
                  style={{ width: 40, height: 40 }}
                >
                  <FontAwesomeIcon icon={item.icon} />
                </div>
                <div>
                  <h6 className="mb-0 fs-10 text-body-tertiary">{item.title}</h6>
                  <h4 className={`mb-0 text-${item.color}`}>
                    <CountUp end={item.value} duration={1} />
                  </h4>
                </div>
              </Flex>
            </Col>
          ))}
        </Row>
      </Card.Body>
    </Card>
  );
};

export default NotificationGreetings;
