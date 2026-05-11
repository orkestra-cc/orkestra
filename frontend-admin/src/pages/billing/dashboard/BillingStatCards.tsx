import { Col, Row, Card, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileInvoice,
  faFileImport,
  faBell,
  faExclamationTriangle,
} from '@fortawesome/free-solid-svg-icons';
import { Link } from 'react-router';
import CountUp from 'react-countup';
import { useGetBillingStatsQuery } from 'store/api/billingApi';
import SubtleBadge from 'components/common/SubtleBadge';
import { ytdRange, lastYearRange } from './dateRanges';

interface StatCardProps {
  title: string;
  value: number;
  icon: any;
  iconColor: string;
  bgColor: string;
  link: string;
  linkText: string;
  prefix?: string;
  suffix?: string;
  decimal?: boolean;
  badge?: {
    text: string;
    bg: 'success' | 'danger' | 'warning' | 'info' | 'primary' | 'secondary';
  };
}

const StatCard: React.FC<StatCardProps> = ({
  title,
  value,
  icon,
  iconColor,
  bgColor,
  link,
  linkText,
  prefix = '',
  suffix = '',
  decimal = false,
  badge,
}) => {
  return (
    <Card className="h-100">
      <Card.Body>
        <div className="d-flex justify-content-between align-items-start">
          <div>
            <h6 className="text-body-tertiary mb-2">
              {title}
              {badge && (
                <SubtleBadge bg={badge.bg} pill className="ms-2 fs-11">
                  {badge.text}
                </SubtleBadge>
              )}
            </h6>
            <h3 className="fw-normal text-body mb-0">
              <CountUp
                start={0}
                end={value}
                duration={2}
                prefix={prefix}
                suffix={suffix}
                separator="."
                decimals={decimal ? 2 : 0}
                decimal=","
              />
            </h3>
          </div>
          <div
            className={`d-flex align-items-center justify-content-center rounded-circle ${bgColor}`}
            style={{ width: 48, height: 48 }}
          >
            <FontAwesomeIcon icon={icon} className={`fs-5 ${iconColor}`} />
          </div>
        </div>
        <Link to={link} className="fw-semibold fs-10 text-nowrap mt-3 d-inline-block">
          {linkText}
          <FontAwesomeIcon icon="angle-right" className="ms-1" transform="down-1" />
        </Link>
      </Card.Body>
    </Card>
  );
};

const BillingStatCards = () => {
  const ytd = ytdRange();
  const lastYear = lastYearRange();
  const { data: ytdStats, isLoading: ytdLoading, error: ytdError } = useGetBillingStatsQuery(ytd);
  const { data: lyStats, isLoading: lyLoading, error: lyError } = useGetBillingStatsQuery(lastYear);

  const isLoading = ytdLoading || lyLoading;
  const error = ytdError || lyError;
  const stats = ytdStats && lyStats ? { ytd: ytdStats, lastYear: lyStats } : null;

  if (isLoading) {
    return (
      <Row className="g-3 mb-3">
        {[1, 2, 3, 4].map((i) => (
          <Col key={i} sm={6} lg={3}>
            <Card className="h-100">
              <Card.Body className="d-flex align-items-center justify-content-center" style={{ minHeight: 120 }}>
                <Spinner animation="border" size="sm" />
              </Card.Body>
            </Card>
          </Col>
        ))}
      </Row>
    );
  }

  if (error || !stats) {
    return (
      <Row className="g-3 mb-3">
        <Col>
          <Card className="bg-warning-subtle">
            <Card.Body className="text-center py-3">
              <FontAwesomeIcon icon={faExclamationTriangle} className="text-warning me-2" />
              Impossibile caricare le statistiche
            </Card.Body>
          </Card>
        </Col>
      </Row>
    );
  }

  const statCards: StatCardProps[] = [
    {
      title: 'Fatture Emesse (YTD)',
      value: stats.ytd.issuedTotal,
      icon: faFileInvoice,
      iconColor: 'text-primary',
      bgColor: 'bg-primary-subtle',
      link: '/billing/invoices/issued',
      linkText: 'Vedi tutte',
      badge: stats.ytd.issuedDraft > 0 ? { text: `${stats.ytd.issuedDraft} bozze`, bg: 'warning' } : undefined,
    },
    {
      title: 'Fatture Ricevute (YTD)',
      value: stats.ytd.receivedTotal,
      icon: faFileImport,
      iconColor: 'text-info',
      bgColor: 'bg-info-subtle',
      link: '/billing/invoices/received',
      linkText: 'Vedi tutte',
      badge: stats.ytd.receivedPending > 0 ? { text: `${stats.ytd.receivedPending} da gestire`, bg: 'info' } : undefined,
    },
    {
      title: 'Notifiche SDI',
      value: stats.lastYear.unprocessedNotifications,
      icon: faBell,
      iconColor: stats.lastYear.unprocessedNotifications > 0 ? 'text-warning' : 'text-success',
      bgColor: stats.lastYear.unprocessedNotifications > 0 ? 'bg-warning-subtle' : 'bg-success-subtle',
      link: '/billing/notifications',
      linkText: 'Gestisci',
      badge: stats.lastYear.unprocessedNotifications > 0 ? { text: 'Da processare', bg: 'warning' } : undefined,
    },
    {
      title: 'Azioni Pendenti',
      value: stats.lastYear.pendingActions,
      icon: faExclamationTriangle,
      iconColor: stats.lastYear.pendingActions > 0 ? 'text-danger' : 'text-success',
      bgColor: stats.lastYear.pendingActions > 0 ? 'bg-danger-subtle' : 'bg-success-subtle',
      link: '/billing/invoices/issued?status=pending',
      linkText: 'Risolvi',
      badge: stats.lastYear.pendingActions > 0 ? { text: 'Urgente', bg: 'danger' } : { text: 'Tutto ok', bg: 'success' },
    },
  ];

  return (
    <Row className="g-3 mb-3">
      {statCards.map((stat) => (
        <Col key={stat.title} sm={6} lg={3}>
          <StatCard {...stat} />
        </Col>
      ))}
    </Row>
  );
};

export default BillingStatCards;
