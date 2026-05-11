import { useMemo } from 'react';
import { Button, Card, Col, Row, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';
import { useDispatch } from 'react-redux';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import {
  complianceApi,
  useGetSoc2EvidenceQuery,
} from 'store/api/complianceApi';
import type { Soc2Evidence } from 'types/compliance';

interface StatCardProps {
  title: string;
  value: number | string;
  icon: IconProp;
  accent: BadgeColor;
  footnote?: React.ReactNode;
  badge?: { text: string; bg: BadgeColor };
}

// Reused Falcon-style card pattern from /admin/tenants. Kept local so the
// SOC2 page owns its visual language — stat cards elsewhere tend to drift.
const StatCard: React.FC<StatCardProps> = ({
  title,
  value,
  icon,
  accent,
  footnote,
  badge,
}) => (
  <Card className="h-100 shadow-none border">
    <Card.Body>
      <div className="d-flex justify-content-between align-items-start">
        <div>
          <h6 className="text-body-tertiary fs-10 text-uppercase mb-2">
            {title}
            {badge && (
              <SubtleBadge bg={badge.bg} pill className="ms-2 fs-11">
                {badge.text}
              </SubtleBadge>
            )}
          </h6>
          <h3 className="fw-normal text-body mb-0">{value}</h3>
        </div>
        <div
          className={`d-flex align-items-center justify-content-center rounded-circle bg-${accent}-subtle`}
          style={{ width: 48, height: 48 }}
        >
          <FontAwesomeIcon icon={icon} className={`fs-5 text-${accent}`} />
        </div>
      </div>
      {footnote && (
        <div className="fs-10 text-body-tertiary mt-3">{footnote}</div>
      )}
    </Card.Body>
  </Card>
);

interface ControlRowProps {
  id: string;
  name: string;
  status: { label: string; color: BadgeColor };
  payload: unknown;
}

// Each control rendered with a badge + an expandable <details> holding the
// raw backend payload. Auditors want to see the raw numbers — the summary
// is for operators at-a-glance, the payload is the receipt.
const ControlRow: React.FC<ControlRowProps> = ({ id, name, status, payload }) => (
  <tr>
    <td className="ps-3 fw-semibold">
      <code className="fs-11 me-2">{id}</code>
      {name}
    </td>
    <td>
      <SubtleBadge bg={status.color} pill>
        {status.label}
      </SubtleBadge>
    </td>
    <td className="pe-3 fs-11">
      <details>
        <summary className="text-primary" style={{ cursor: 'pointer' }}>
          Show payload
        </summary>
        <pre
          className="bg-body-tertiary rounded p-2 mt-2 mb-0 fs-11"
          style={{ maxHeight: 240, overflow: 'auto' }}
        >
          {JSON.stringify(payload, null, 2)}
        </pre>
      </details>
    </td>
  </tr>
);

// --- control-level status heuristics ---------------------------------------
// Light-weight thresholds that translate the raw payload into a Pass/Warn/
// Alert surface. These are intentionally conservative — auditors ask us to
// surface "is this green or not" at a glance; the raw payload behind the
// expander is the source of truth.

type StatusKey = 'pass' | 'warn' | 'alert' | 'unknown';

const statusColor: Record<StatusKey, BadgeColor> = {
  pass: 'success',
  warn: 'warning',
  alert: 'danger',
  unknown: 'secondary',
};

const statusLabel: Record<StatusKey, string> = {
  pass: 'Healthy',
  warn: 'Attention',
  alert: 'Critical',
  unknown: 'No data',
};

interface ControlViewModel {
  id: string;
  name: string;
  status: StatusKey;
  payload: unknown;
}

function classifyControl(id: string, payload: unknown): StatusKey {
  if (payload === null || payload === undefined) return 'unknown';
  if (typeof payload !== 'object') return 'unknown';
  const p = payload as Record<string, unknown>;
  switch (id) {
    case 'CC6.1_logical_access': {
      const total = Number(p.total ?? 0);
      return total > 0 ? 'pass' : 'warn';
    }
    case 'CC6.6_account_management': {
      const pct = Number(p.percentCovered ?? 0);
      if (pct >= 100) return 'pass';
      if (pct >= 80) return 'warn';
      return 'alert';
    }
    case 'CC7.2_monitoring': {
      const last24 = Number(p.failedLoginsLast24h ?? 0);
      if (last24 < 50) return 'pass';
      if (last24 < 500) return 'warn';
      return 'alert';
    }
    case 'CC6.8_data_protection': {
      const active = Number(p.active ?? 0);
      return active > 0 ? 'pass' : 'warn';
    }
    case 'CC7.2_audit_coverage': {
      const rows = Number(p.rowsLast24h ?? 0);
      return rows > 0 ? 'pass' : 'alert';
    }
    default:
      return 'unknown';
  }
}

const CONTROL_NAMES: Record<string, string> = {
  'CC6.1_logical_access': 'Logical access — privileged accounts',
  'CC6.6_account_management': 'Account management — MFA coverage',
  'CC6.8_data_protection': 'Data protection — KMS key lifecycle',
  'CC7.2_monitoring': 'Monitoring — failed-login trend',
  'CC7.2_audit_coverage': 'Monitoring — audit trail coverage',
};

function buildControlViewModels(ev?: Soc2Evidence): ControlViewModel[] {
  if (!ev?.controls) return [];
  return Object.entries(ev.controls).map(([id, payload]) => ({
    id,
    name: CONTROL_NAMES[id] ?? id,
    status: classifyControl(id, payload),
    payload,
  }));
}

const Soc2EvidencePage: React.FC = () => {
  const dispatch = useDispatch();
  const { data, isFetching, error, refetch } = useGetSoc2EvidenceQuery();
  const controls = useMemo(() => buildControlViewModels(data), [data]);

  const summary = data?.summary ?? {};
  const privileged = summary.privileged_users ?? 0;
  const privMFA = summary.privileged_with_mfa ?? 0;
  const coveragePct = privileged === 0 ? 100 : Math.round((privMFA / privileged) * 100);
  const coverageBadge: BadgeColor = coveragePct === 100 ? 'success' : coveragePct >= 80 ? 'warning' : 'danger';
  const failed24 = summary.failed_logins_24h ?? 0;
  const auditRows = summary.audit_rows_24h ?? 0;
  const kmsActive = summary.kms_keys_active ?? 0;
  const kmsShredded = summary.kms_keys_shredded ?? 0;

  if (error) {
    return (
      <Card>
        <Card.Body className="text-center text-danger py-5">
          Failed to load SOC2 evidence. You need the{' '}
          <code>system.compliance.audit.read</code> permission to view this page.
        </Card.Body>
      </Card>
    );
  }

  return (
    <>
      <Row className="g-3 mb-3">
        <Col>
          <Card className="shadow-none border">
            <Card.Body className="d-flex justify-content-between align-items-center flex-wrap gap-3">
              <div>
                <h5 className="mb-1">
                  <FontAwesomeIcon icon="shield-alt" className="me-2 text-primary" />
                  SOC2 Evidence
                </h5>
                <p className="fs-10 mb-0 text-body-secondary">
                  Point-in-time aggregate of the CC-class controls auditors commonly sample.
                  Every refresh recomputes from source — two auditors hitting this one minute
                  apart see the same answer when state hasn't changed.
                </p>
                {data?.generatedAt && (
                  <p className="fs-11 mb-0 text-body-tertiary mt-1">
                    Generated at {new Date(data.generatedAt).toLocaleString()}
                  </p>
                )}
              </div>
              <Button
                variant="outline-primary"
                size="sm"
                onClick={() => {
                  // Drop the cached snapshot, then refetch so the button feels
                  // like it re-runs evidence collection end-to-end.
                  dispatch(
                    complianceApi.util.invalidateTags([
                      { type: 'Soc2Evidence', id: 'SNAPSHOT' },
                    ]),
                  );
                  refetch();
                }}
                disabled={isFetching}
              >
                {isFetching ? (
                  <>
                    <Spinner animation="border" size="sm" className="me-2" /> Regenerating…
                  </>
                ) : (
                  <>
                    <FontAwesomeIcon icon="redo" className="me-2" /> Regenerate
                  </>
                )}
              </Button>
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <Row className="g-3 mb-3">
        <Col md={6} xl={4}>
          <StatCard
            title="Privileged users"
            value={privileged}
            icon="user-shield"
            accent="primary"
            footnote="super_admin + administrator + developer roles"
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title="Privileged with MFA"
            value={`${privMFA} / ${privileged}`}
            icon="key"
            accent={coverageBadge}
            badge={
              privileged > 0
                ? { text: `${coveragePct}%`, bg: coverageBadge }
                : undefined
            }
            footnote={
              privileged === 0
                ? 'No privileged users to evaluate'
                : coveragePct === 100
                  ? '100% — target met'
                  : `${privileged - privMFA} account${privileged - privMFA === 1 ? '' : 's'} missing a second factor`
            }
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title="Failed logins (24h)"
            value={failed24}
            icon="bell"
            accent={failed24 < 50 ? 'success' : failed24 < 500 ? 'warning' : 'danger'}
            footnote="CC7.2 — spikes indicate credential stuffing or integration drift"
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title="Audit rows (24h)"
            value={auditRows}
            icon="clipboard-list"
            accent={auditRows > 0 ? 'success' : 'danger'}
            footnote={
              auditRows === 0
                ? 'No events in the last 24 hours — pipeline may be unhealthy'
                : 'Audit pipeline actively capturing events'
            }
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title="KMS keys active"
            value={kmsActive}
            icon="key"
            accent={kmsActive > 0 ? 'success' : 'secondary'}
            footnote="Per-tenant envelopes ready for crypto-shred on purge"
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title="KMS keys shredded"
            value={kmsShredded}
            icon="trash"
            accent="secondary"
            footnote="Tenants purged via crypto-shred (unrecoverable by design)"
          />
        </Col>
      </Row>

      <Card className="shadow-none border">
        <Card.Header className="border-bottom border-200 px-4 py-3">
          <div className="d-flex justify-content-between align-items-center">
            <div>
              <h6 className="mb-0">Controls</h6>
              <p className="fs-11 mb-0 text-body-tertiary">
                Status heuristics are conservative — the payload panel is the
                source of truth for auditors.
              </p>
            </div>
          </div>
        </Card.Header>
        <Card.Body className="p-0">
          {isFetching && !data ? (
            <div className="text-center py-5">
              <Spinner animation="border" size="sm" />
            </div>
          ) : (
            <Table responsive size="sm" className="fs-10 mb-0">
              <thead className="bg-body-tertiary">
                <tr>
                  <th className="ps-3">Control</th>
                  <th>Status</th>
                  <th className="pe-3">Payload</th>
                </tr>
              </thead>
              <tbody>
                {controls.map((c) => (
                  <ControlRow
                    key={c.id}
                    id={c.id}
                    name={c.name}
                    status={{ label: statusLabel[c.status], color: statusColor[c.status] }}
                    payload={c.payload}
                  />
                ))}
                {controls.length === 0 && (
                  <tr>
                    <td colSpan={3} className="text-center text-muted py-4">
                      No controls reported.
                    </td>
                  </tr>
                )}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>
    </>
  );
};

export default Soc2EvidencePage;
