import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileInvoiceDollar,
  faChartLine,
  faUsers,
  faArrowLeft
} from '@fortawesome/free-solid-svg-icons';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Flex from 'components/common/Flex';
import { useGetBillingStatsQuery } from 'store/api/billingApi';
import CountUp from 'react-countup';
import { formatCurrency } from 'types/billing';

// All-time fromDate so the rollup mirrors the (un-date-scoped) table below;
// without it the backend defaults to current month and reads zero.
const STATS_FROM_DATE_ALL_TIME = '2000-01-01';

const IssuedInvoiceGreetings = () => {
  const { t } = useTranslation();
  const { data: stats } = useGetBillingStatsQuery({
    fromDate: STATS_FROM_DATE_ALL_TIME
  });

  const statItems = [
    {
      title: t('billing.issued.stats.total'),
      value: stats?.issuedTotal || 0,
      color: 'primary',
      icon: faFileInvoiceDollar
    },
    {
      title: t('billing.issued.stats.draft'),
      value: stats?.issuedDraft || 0,
      color: 'warning',
      icon: faFileInvoiceDollar
    },
    {
      title: t('billing.issued.stats.sent'),
      value: stats?.issuedSent || 0,
      color: 'info',
      icon: faChartLine
    },
    {
      title: t('billing.issued.stats.delivered'),
      value: stats?.issuedDelivered || 0,
      color: 'success',
      icon: faUsers
    }
  ];

  return (
    <Card>
      <OrkestraCardHeader
        title={
          <Flex alignItems="center">
            <Link
              to="/billing/dashboard"
              className="text-body-tertiary me-2"
              title={t('billing.common.backToDashboard')}
            >
              <FontAwesomeIcon icon={faArrowLeft} />
            </Link>
            {t('billing.issued.title')}
          </Flex>
        }
        titleTag="h5"
        className="py-2"
        light
        endEl={
          stats && (
            <span className="text-body-tertiary fs-10">
              {t('billing.common.volumeLabel')}{' '}
              <span className="fw-medium text-primary">
                {formatCurrency(stats.issuedAmount)}
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

export default IssuedInvoiceGreetings;
