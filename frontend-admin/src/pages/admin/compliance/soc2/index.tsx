import { Alert, Button, Col, Row, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faClipboardList,
  faKey,
  faRotate,
  faShieldHalved,
  faTriangleExclamation,
  faUserLock,
  faUserShield
} from '@fortawesome/free-solid-svg-icons';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';
import ComplianceStatCard from 'pages/admin/compliance/ComplianceStatCard';
import ComplianceSection from 'pages/admin/compliance/ComplianceSection';
import type { BadgeColor } from 'components/common/SubtleBadge';
import { useSoc2EvidenceQuery } from 'store/api/complianceApi';

// SOC2EvidencePage renders the on-demand SOC2 control snapshot
// (GET /v1/admin/compliance/soc2/evidence): a KPI row from the flat summary
// map and a per-control breakdown of the nested CC-class attributes. The
// endpoint 404s when compliance.soc2_enabled is off — handled gracefully.

// Friendly presentation for the known summary metrics; unknown keys fall back
// to a humanized label + neutral styling so a new backend metric still renders.
const SUMMARY_META: Record<
  string,
  { label: string; icon: IconProp; color: BadgeColor }
> = {
  privileged_users: {
    label: 'Privileged Users',
    icon: faUserShield,
    color: 'warning'
  },
  privileged_with_mfa: {
    label: 'Privileged with MFA',
    icon: faUserLock,
    color: 'success'
  },
  failed_logins_24h: {
    label: 'Failed Logins (24h)',
    icon: faTriangleExclamation,
    color: 'danger'
  },
  kms_keys_active: { label: 'KMS Keys Active', icon: faKey, color: 'info' },
  kms_keys_shredded: {
    label: 'KMS Keys Shredded',
    icon: faKey,
    color: 'secondary'
  },
  audit_rows_24h: {
    label: 'Audit Rows (24h)',
    icon: faClipboardList,
    color: 'primary'
  }
};

const humanize = (key: string): string =>
  key
    .replace(/_/g, ' ')
    .replace(/\b\w/g, c => c.toUpperCase())
    .trim();

// "CC6.1_logical_access" → "CC6.1 · Logical Access".
const controlTitle = (key: string): string => {
  const [code, ...rest] = key.split('_');
  const label = rest.join(' ').replace(/\b\w/g, c => c.toUpperCase());
  return label ? `${code} · ${label}` : code;
};

const renderValue = (value: unknown): string => {
  if (value === null || value === undefined) return '—';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
};

const SOC2EvidencePage = () => {
  const { data, isLoading, isFetching, error, refetch } =
    useSoc2EvidenceQuery();

  const isDisabled =
    !!error &&
    'status' in error &&
    (error as { status: number }).status === 404;

  return (
    <>
      <div className="mb-3 d-flex align-items-start justify-content-between flex-wrap gap-2">
        <div>
          <h2 className="mb-1">
            <FontAwesomeIcon
              icon={faShieldHalved}
              className="me-2 text-primary"
            />
            SOC2 Evidence
          </h2>
          <p className="text-muted mb-0">
            Point-in-time snapshot of Trust Service Criteria controls —
            privileged access, MFA coverage, failed-login trends, KMS lifecycle,
            and audit-trail health. Recomputed on every load.
          </p>
        </div>
        <Button
          variant="falcon-default"
          size="sm"
          onClick={() => refetch()}
          disabled={isFetching}
        >
          <FontAwesomeIcon icon={faRotate} className="me-2" spin={isFetching} />
          Refresh
        </Button>
      </div>

      {isLoading ? (
        <div className="text-center py-6">
          <Spinner animation="border" />
        </div>
      ) : isDisabled ? (
        <Alert variant="warning" className="d-flex align-items-start">
          <FontAwesomeIcon icon={faTriangleExclamation} className="me-3 mt-1" />
          <div>
            <Alert.Heading className="h6 mb-1">
              SOC2 evidence is disabled
            </Alert.Heading>
            <p className="mb-0 small">
              Enable the <code>soc2_enabled</code> flag on the{' '}
              <strong>compliance</strong> module at <code>/admin/modules</code>{' '}
              to generate evidence snapshots.
            </p>
          </div>
        </Alert>
      ) : error || !data ? (
        <Alert variant="danger">
          <FontAwesomeIcon icon={faTriangleExclamation} className="me-2" />
          Failed to generate the SOC2 evidence snapshot.
        </Alert>
      ) : (
        <>
          <p className="text-muted fs-11 mb-3">
            Generated {new Date(data.generatedAt).toLocaleString()}
          </p>

          <Row className="g-3 mb-3">
            {Object.entries(data.summary).map(([key, value]) => {
              const meta = SUMMARY_META[key];
              return (
                <Col md={6} lg={3} key={key}>
                  <ComplianceStatCard
                    title={meta?.label ?? humanize(key)}
                    value={value}
                    icon={meta?.icon ?? faShieldHalved}
                    color={meta?.color ?? 'secondary'}
                  />
                </Col>
              );
            })}
          </Row>

          <Row className="g-3">
            {Object.entries(data.controls).map(([key, attrs]) => (
              <Col lg={6} key={key}>
                <ComplianceSection
                  icon={faShieldHalved}
                  title={controlTitle(key)}
                >
                  <Table size="sm" className="mb-0 fs-10">
                    <tbody>
                      {Object.entries(attrs).map(([attr, value]) => (
                        <tr key={attr}>
                          <td className="text-muted">{humanize(attr)}</td>
                          <td className="text-end fw-semibold text-900 font-monospace">
                            {renderValue(value)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                </ComplianceSection>
              </Col>
            ))}
          </Row>
        </>
      )}
    </>
  );
};

export default SOC2EvidencePage;
