import { useMemo } from 'react';
import { Button, Card, Col, Row, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';
import { useDispatch } from 'react-redux';
import { Trans, useTranslation } from 'react-i18next';

type TFunction = ReturnType<typeof useTranslation>['t'];
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import {
  complianceApi,
  useGetSoc2EvidenceQuery
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

// Reused Orkestra-style card pattern from /admin/tenants. Kept local so the
// SOC2 page owns its visual language — stat cards elsewhere tend to drift.
const StatCard: React.FC<StatCardProps> = ({
  title,
  value,
  icon,
  accent,
  footnote,
  badge
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
const ControlRow: React.FC<ControlRowProps> = ({
  id,
  name,
  status,
  payload
}) => {
  const { t } = useTranslation();
  return (
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
            {t('compliance.soc2.showPayload')}
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
};

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
  unknown: 'secondary'
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

// Mirrors backend SOC2 control IDs → i18n key segments. Dots in the source
// IDs are flattened to underscores so the keys nest cleanly under
// compliance.soc2.controlNames.*.
const CONTROL_NAME_KEYS: Record<string, string> = {
  'CC6.1_logical_access': 'CC6_1_logical_access',
  'CC6.6_account_management': 'CC6_6_account_management',
  'CC6.8_data_protection': 'CC6_8_data_protection',
  'CC7.2_monitoring': 'CC7_2_monitoring',
  'CC7.2_audit_coverage': 'CC7_2_audit_coverage'
};

function buildControlViewModels(
  ev: Soc2Evidence | undefined,
  t: TFunction
): ControlViewModel[] {
  if (!ev?.controls) return [];
  return Object.entries(ev.controls).map(([id, payload]) => {
    const nameKey = CONTROL_NAME_KEYS[id];
    return {
      id,
      name: nameKey
        ? t(`compliance.soc2.controlNames.${nameKey}`, { defaultValue: id })
        : id,
      status: classifyControl(id, payload),
      payload
    };
  });
}

const Soc2EvidencePage: React.FC = () => {
  const { t } = useTranslation();
  const dispatch = useDispatch();
  const { data, isFetching, error, refetch } = useGetSoc2EvidenceQuery();
  const controls = useMemo(() => buildControlViewModels(data, t), [data, t]);

  const statusLabel = useMemo<Record<StatusKey, string>>(
    () => ({
      pass: t('compliance.soc2.statusLabels.pass'),
      warn: t('compliance.soc2.statusLabels.warn'),
      alert: t('compliance.soc2.statusLabels.alert'),
      unknown: t('compliance.soc2.statusLabels.unknown')
    }),
    [t]
  );

  const summary = data?.summary ?? {};
  const privileged = summary.privileged_users ?? 0;
  const privMFA = summary.privileged_with_mfa ?? 0;
  const coveragePct =
    privileged === 0 ? 100 : Math.round((privMFA / privileged) * 100);
  const coverageBadge: BadgeColor =
    coveragePct === 100 ? 'success' : coveragePct >= 80 ? 'warning' : 'danger';
  const failed24 = summary.failed_logins_24h ?? 0;
  const auditRows = summary.audit_rows_24h ?? 0;
  const kmsActive = summary.kms_keys_active ?? 0;
  const kmsShredded = summary.kms_keys_shredded ?? 0;

  if (error) {
    return (
      <Card>
        <Card.Body className="text-center text-danger py-5">
          <Trans
            i18nKey="compliance.soc2.loadError"
            components={{ code: <code /> }}
          />
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
                  <FontAwesomeIcon
                    icon="shield-alt"
                    className="me-2 text-primary"
                  />
                  {t('compliance.soc2.title')}
                </h5>
                <p className="fs-10 mb-0 text-body-secondary">
                  {t('compliance.soc2.description')}
                </p>
                {data?.generatedAt && (
                  <p className="fs-11 mb-0 text-body-tertiary mt-1">
                    {t('compliance.soc2.generatedAt', {
                      date: new Date(data.generatedAt).toLocaleString()
                    })}
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
                      { type: 'Soc2Evidence', id: 'SNAPSHOT' }
                    ])
                  );
                  refetch();
                }}
                disabled={isFetching}
              >
                {isFetching ? (
                  <>
                    <Spinner animation="border" size="sm" className="me-2" />{' '}
                    {t('compliance.soc2.regenerating')}
                  </>
                ) : (
                  <>
                    <FontAwesomeIcon icon="redo" className="me-2" />{' '}
                    {t('compliance.soc2.regenerate')}
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
            title={t('compliance.soc2.stats.privileged')}
            value={privileged}
            icon="user-shield"
            accent="primary"
            footnote={t('compliance.soc2.stats.privilegedFootnote')}
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title={t('compliance.soc2.stats.privilegedWithMfa')}
            value={t('compliance.soc2.mfaCoverageFraction', {
              covered: privMFA,
              total: privileged
            })}
            icon="key"
            accent={coverageBadge}
            badge={
              privileged > 0
                ? {
                    text: t('compliance.soc2.coveragePercentBadge', {
                      percent: coveragePct
                    }),
                    bg: coverageBadge
                  }
                : undefined
            }
            footnote={
              privileged === 0
                ? t('compliance.soc2.stats.noPrivToEvaluate')
                : coveragePct === 100
                  ? t('compliance.soc2.stats.targetMet')
                  : t('compliance.soc2.stats.missingSecondFactor', {
                      count: privileged - privMFA
                    })
            }
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title={t('compliance.soc2.stats.failedLogins24h')}
            value={failed24}
            icon="bell"
            accent={
              failed24 < 50 ? 'success' : failed24 < 500 ? 'warning' : 'danger'
            }
            footnote={t('compliance.soc2.stats.failedLoginsFootnote')}
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title={t('compliance.soc2.stats.auditRows24h')}
            value={auditRows}
            icon="clipboard-list"
            accent={auditRows > 0 ? 'success' : 'danger'}
            footnote={
              auditRows === 0
                ? t('compliance.soc2.stats.auditEmptyFootnote')
                : t('compliance.soc2.stats.auditActiveFootnote')
            }
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title={t('compliance.soc2.stats.kmsActive')}
            value={kmsActive}
            icon="key"
            accent={kmsActive > 0 ? 'success' : 'secondary'}
            footnote={t('compliance.soc2.stats.kmsActiveFootnote')}
          />
        </Col>
        <Col md={6} xl={4}>
          <StatCard
            title={t('compliance.soc2.stats.kmsShredded')}
            value={kmsShredded}
            icon="trash"
            accent="secondary"
            footnote={t('compliance.soc2.stats.kmsShreddedFootnote')}
          />
        </Col>
      </Row>

      <Card className="shadow-none border">
        <Card.Header className="border-bottom border-200 px-4 py-3">
          <div className="d-flex justify-content-between align-items-center">
            <div>
              <h6 className="mb-0">{t('compliance.soc2.controlsHeading')}</h6>
              <p className="fs-11 mb-0 text-body-tertiary">
                {t('compliance.soc2.controlsSubtitle')}
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
                  <th className="ps-3">{t('compliance.soc2.colControl')}</th>
                  <th>{t('compliance.soc2.colStatus')}</th>
                  <th className="pe-3">{t('compliance.soc2.colPayload')}</th>
                </tr>
              </thead>
              <tbody>
                {controls.map(c => (
                  <ControlRow
                    key={c.id}
                    id={c.id}
                    name={c.name}
                    status={{
                      label: statusLabel[c.status],
                      color: statusColor[c.status]
                    }}
                    payload={c.payload}
                  />
                ))}
                {controls.length === 0 && (
                  <tr>
                    <td colSpan={3} className="text-center text-muted py-4">
                      {t('compliance.soc2.controlsEmpty')}
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
