import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileImport,
  faClock,
  faCheck,
  faTimes,
  faArrowLeft
} from '@fortawesome/free-solid-svg-icons';
import { Link } from 'react-router';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';
import { useGetBillingStatsQuery } from 'store/api/billingApi';
import CountUp from 'react-countup';
import { formatCurrency } from 'types/billing';

// All-time fromDate so the rollup mirrors the (un-date-scoped) table below;
// without it the backend defaults to current month and reads zero.
const STATS_FROM_DATE_ALL_TIME = '2000-01-01';

const ReceivedInvoiceGreetings = () => {
  const { data: stats } = useGetBillingStatsQuery({
    fromDate: STATS_FROM_DATE_ALL_TIME
  });

  const statItems = [
    {
      title: 'Totale Ricevute',
      value: stats?.receivedTotal || 0,
      color: 'success',
      icon: faFileImport
    },
    {
      title: 'In Attesa',
      value: stats?.receivedPending || 0,
      color: 'warning',
      icon: faClock
    },
    {
      title: 'Accettate',
      value: stats?.receivedAccepted || 0,
      color: 'primary',
      icon: faCheck
    },
    {
      title: 'Rifiutate',
      value: stats?.receivedRejected || 0,
      color: 'danger',
      icon: faTimes
    }
  ];

  return (
    <Card>
      <OrkestraCardHeader
        title={
          <Flex alignItems="center">
            <Link to="/billing/dashboard" className="text-body-tertiary me-2">
              <FontAwesomeIcon icon={faArrowLeft} />
            </Link>
            Fatture Ricevute
          </Flex>
        }
        titleTag="h5"
        className="py-2"
        light
        endEl={
          stats && (
            <span className="text-body-tertiary fs-10">
              Volume:{' '}
              <span className="fw-medium text-success">
                {formatCurrency(stats.receivedAmount)}
              </span>
            </span>
          )
        }
      />
      <Card.Body className="py-3">
        <Row className="g-3">
          {statItems.map(item => (
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
                  <h6 className="mb-0 fs-10 text-body-tertiary">
                    {item.title}
                  </h6>
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

export default ReceivedInvoiceGreetings;
