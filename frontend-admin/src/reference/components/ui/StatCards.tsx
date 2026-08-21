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
import StatCardPair from 'components/common/StatCardPair';
import SectionCard from 'components/common/SectionCard';
import { Col, Row, Table } from 'react-bootstrap';
import { Link } from 'react-router';

// StatCards is the Orkestra reference showcase for the shared ERP-style KPI
// tile (StatCard) and its companion titled panel (SectionCard) — the canonical
// summary-row + section primitives used across admin dashboards
// (/admin/compliance, /admin/compliance/soc2, /admin/tenants). Prefer them over
// a bespoke per-page stat card so every dashboard's KPI row matches.

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

const accentCode = `
<Row className="g-3">
  {/* At rest: neutral edge. The non-zero counter earns its hue. */}
  <Col md={6} lg={4}>
    <StatCard
      title="Pending Erasures"
      value={0}
      icon={faUserSlash}
      color="warning"
      subtitle="Awaiting review"
    />
  </Col>
  <Col md={6} lg={4}>
    <StatCard
      title="Pending Erasures"
      value={3}
      icon={faUserSlash}
      color="warning"
      accent="warning"
      subtitle="Awaiting review"
    />
  </Col>
</Row>
`;

const ribbonCode = `
<Row className="g-3">
  <Col md={6} lg={4}>
    <StatCard
      title="Pending Erasures"
      value={3}
      icon={faUserSlash}
      color="warning"
      accent="warning"
      subtitle="Awaiting review"
      badge={{ text: 'Review' }}
    />
  </Col>
  <Col md={6} lg={4}>
    <StatCard
      title="Active Legal Holds"
      value={1}
      icon={faGavel}
      color="danger"
      accent="danger"
      subtitle="Blocking erasure"
      badge={{ text: 'Blocked' }}
    />
  </Col>
  <Col md={6} lg={4}>
    <StatCard
      title="Retention Candidates"
      value={12}
      icon={faClockRotateLeft}
      color="info"
      accent="info"
      subtitle="Past retention window"
      badge={{ text: 'Overdue', bg: 'secondary' }}
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

const footerCode = `
<Row className="g-3">
  <Col md={6} lg={4}>
    <StatCard
      title="Audit Rows (24h)"
      value={8}
      icon={faClipboardList}
      color="primary"
      footer={
        <Link to="/admin/compliance" className="fw-semibold fs-10 text-nowrap">
          View all
        </Link>
      }
    />
  </Col>
  <Col md={6} lg={4}>
    <StatCard
      title="Failed Logins (24h)"
      value={3}
      icon={faTriangleExclamation}
      color="danger"
      accent="danger"
      badge={{ text: 'Review' }}
      footer={
        <Link to="/admin/compliance" className="fw-semibold fs-10 text-nowrap">
          Investigate
        </Link>
      }
    />
  </Col>
</Row>
`;

const pairCode = `
<Row className="g-3">
  <Col md={6} lg={4}>
    <StatCardPair
      title="Access Reviews (30d)"
      halves={[
        {
          title: 'Granted',
          value: 12,
          icon: faUserLock,
          color: 'success',
          footer: <Link to="/admin/roles" className="fw-semibold fs-10 text-nowrap">View all</Link>
        },
        {
          title: 'Revoked',
          value: 3,
          icon: faUserSlash,
          color: 'danger',
          badge: { text: '1 pending' },
          footer: <Link to="/admin/roles" className="fw-semibold fs-10 text-nowrap">View all</Link>
        }
      ]}
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
        description="The Orkestra ERP-style KPI tile (StatCard) and its companion titled panel (SectionCard) — the canonical summary-row and section primitives for admin dashboards. A 4px left accent edge (neutral at rest, colored only when the state earns it), a faded 2x icon anchored bottom-right and kept quieter than the datum it labels, a big headline value, an optional subtitle, an optional drill-down footer, and an attention flag rendered as a diagonal corner ribbon."
        className="mb-3"
      />

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Color variants" light={false}>
          <p className="mb-0">
            The <code>color</code> prop tints the faded icon (and is the
            ribbon's fallback hue). It accepts any <code>BadgeColor</code> (
            <code>primary</code>, <code>success</code>, <code>info</code>,{' '}
            <code>warning</code>, <code>danger</code>, <code>secondary</code>,
            …). The accent edge stays neutral — see the next section.
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
          title="Status accent — earned by state"
          light={false}
        >
          <p className="mb-0">
            The 4px left edge is the tile's status channel and is{' '}
            <strong>neutral at rest</strong>. Pass <code>accent</code> only
            when the metric's current state earns it — a non-zero pending
            counter, a degraded health check. Status colors mean status: never
            use the accent as category identity.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={accentCode}
          language="jsx"
          scope={{ StatCard, Row, Col, faUserSlash }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header
          title="Subtitle + corner ribbon"
          light={false}
        >
          <p className="mb-0">
            Pass <code>subtitle</code> for a muted caption under the value, and{' '}
            <code>badge</code> (<code>{'{ text, bg? }'}</code>) to raise a
            corner ribbon flag — render it only when the metric needs attention.
            Keep the ribbon text short (one word reads best).
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={ribbonCode}
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

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header
          title="Drill-down footer"
          light={false}
        >
          <p className="mb-0">
            Pass <code>footer</code> for a link to the page the metric counts.
            It is pinned to the bottom of the text column, so a row of tiles
            shares one link line however tall their values and subtitles run.
            A tile whose metric has no page to drill into simply omits it —
            never point one at a list the rows provably are not in.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={footerCode}
          language="jsx"
          scope={{
            StatCard,
            Row,
            Col,
            Link,
            faClipboardList,
            faTriangleExclamation
          }}
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header
          title="StatCardPair — two metrics, one tile"
          light={false}
        >
          <p className="mb-0">
            <code>StatCardPair</code> carries a pair of figures an operator
            reads <em>against each other</em> — issued vs received, granted vs
            revoked. Two unrelated metrics belong in two{' '}
            <code>StatCard</code>s instead, where each gets its own accent edge
            and ribbon. It shares this tile's visual language so the two sit in
            one KPI row without reading as different components, and it drops
            the large icon entirely: each half carries its own, and a third
            would be the tallest thing in the card.
          </p>
          <p className="mb-0 mt-2">
            One deliberate divergence: the corner ribbon is a{' '}
            <strong>card-level</strong> element, so a two-metric card would
            need two of them overlapping in the same corner. A half raises its
            flag as an inline pill instead — if a metric needs the ribbon's
            weight, it needs its own <code>StatCard</code>.
          </p>
        </OrkestraComponentCard.Header>
        <OrkestraComponentCard.Body
          code={pairCode}
          language="jsx"
          scope={{ StatCardPair, Row, Col, Link, faUserLock, faUserSlash }}
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
