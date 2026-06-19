import {
  faClipboardList,
  faClockRotateLeft,
  faGavel,
  faKey,
  faShieldHalved,
  faTriangleExclamation,
  faUserLock,
  faUserShield,
  faUserSlash
} from '@fortawesome/free-solid-svg-icons';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import StatCard from 'components/common/StatCard';
import SectionCard from 'components/common/SectionCard';
import { Col, Row, Table } from 'react-bootstrap';

// StatCards is the Orkestra reference showcase for the shared ERP-style KPI
// tile (StatCard) and its companion titled panel (SectionCard). These are the
// canonical summary-row + section primitives used across admin dashboards
// (/admin/compliance, /admin/compliance/soc2, /admin/tenants). Prefer them
// over a bespoke per-page stat card so every dashboard's KPI row matches.

const colorVariantsCode = `
<Row className="g-3">
  {[
    { color: 'primary', icon: faClipboardList, title: 'Audit Rows (24h)' },
    { color: 'success', icon: faUserLock, title: 'Privileged with MFA' },
    { color: 'info', icon: faKey, title: 'KMS Keys Active' },
    { color: 'warning', icon: faUserShield, title: 'Privileged Users' },
    { color: 'danger', icon: faTriangleExclamation, title: 'Failed Logins (24h)' },
    { color: 'secondary', icon: faKey, title: 'KMS Keys Shredded' }
  ].map(card => (
    <Col md={6} lg={4} key={card.color}>
      <StatCard
        title={card.title}
        value={Math.floor(Math.random() * 40)}
        icon={card.icon}
        color={card.color}
      />
    </Col>
  ))}
</Row>
`;

const subtitleBadgeCode = `
<Row className="g-3">
  <Col md={6} lg={4}>
    <StatCard
      title="Pending Erasures"
      value={3}
      icon={faUserSlash}
      color="warning"
      subtitle="Awaiting review"
      badge={{ text: 'Needs attention' }}
    />
  </Col>
  <Col md={6} lg={4}>
    <StatCard
      title="Active Legal Holds"
      value={1}
      icon={faGavel}
      color="danger"
      subtitle="Blocking erasure"
      badge={{ text: 'Erasure blocked' }}
    />
  </Col>
  <Col md={6} lg={4}>
    <StatCard
      title="Retention Candidates"
      value={12}
      icon={faClockRotateLeft}
      color="info"
      subtitle="Past retention window"
      badge={{ text: '12 subjects', bg: 'secondary' }}
    />
  </Col>
</Row>
`;

const loadingCode = `
<Row className="g-3">
  <Col md={6} lg={4}>
    <StatCard
      title="Audit Events"
      value={0}
      icon={faClipboardList}
      color="primary"
      subtitle="Recorded total"
      loading
    />
  </Col>
</Row>
`;

const sectionCardCode = `
<SectionCard icon={faShieldHalved} title="CC6.1 · Logical Access">
  <Table size="sm" className="mb-0 fs-10">
    <tbody>
      <tr>
        <td className="text-muted">By Role</td>
        <td className="text-end fw-semibold text-900 font-monospace">
          administrator: 0, super_admin: 1
        </td>
      </tr>
      <tr>
        <td className="text-muted">Total</td>
        <td className="text-end fw-semibold text-900 font-monospace">1</td>
      </tr>
    </tbody>
  </Table>
</SectionCard>
`;

const StatCards = () => {
  return (
    <>
      <PageHeader
        title="Stat Cards"
        description="The Orkestra ERP-style KPI tile (StatCard) and its companion titled panel (SectionCard) — the canonical summary-row and section primitives for admin dashboards. A full 4px color-accented border, a large faded 3x icon, a big headline value, an optional subtitle, and an attention badge that only shows when it matters."
        className="mb-3"
      />

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Color variants" light={false}>
          <p className="mb-0">
            The <code>color</code> prop drives the border, icon, and badge tint.
            It accepts any <code>BadgeColor</code> (<code>primary</code>,{' '}
            <code>success</code>, <code>info</code>, <code>warning</code>,{' '}
            <code>danger</code>, <code>secondary</code>, …).
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={colorVariantsCode}
          language="jsx"
          scope={{
            StatCard,
            Row,
            Col,
            faClipboardList,
            faUserLock,
            faKey,
            faUserShield,
            faTriangleExclamation
          }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header
          title="Subtitle + attention badge"
          light={false}
        >
          <p className="mb-0">
            Pass <code>subtitle</code> for a muted caption under the value, and{' '}
            <code>badge</code> (<code>{'{ text, bg? }'}</code>) to flag a metric
            that needs attention — render it only when the condition is true.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={subtitleBadgeCode}
          language="jsx"
          scope={{
            StatCard,
            Row,
            Col,
            faUserSlash,
            faGavel,
            faClockRotateLeft
          }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Loading state" light={false}>
          <p className="mb-0">
            Pass <code>loading</code> to swap the value for a spinner while the
            backing query resolves.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={loadingCode}
          language="jsx"
          scope={{ StatCard, Row, Col, faClipboardList }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard noGuttersBottom>
        <OrkestraComponentCard.Header
          title="SectionCard companion"
          light={false}
        >
          <p className="mb-0">
            <code>SectionCard</code> is the titled panel that sits beneath the
            KPI row: a tinted header (<code>bg-body-tertiary</code>) with an
            icon + title, an optional <code>headerEnd</code> actions slot, and a
            plain body.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={sectionCardCode}
          language="jsx"
          scope={{ SectionCard, Table, faShieldHalved }}
        />
      </OrkestraComponentCard>
    </>
  );
};

export default StatCards;
