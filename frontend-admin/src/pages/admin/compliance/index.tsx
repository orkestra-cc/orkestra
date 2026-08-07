import { Card, Col, Nav, Row, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faClipboardList,
  faClockRotateLeft,
  faGavel,
  faUserSlash
} from '@fortawesome/free-solid-svg-icons';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  useListAuditEventsQuery,
  useListErasureRequestsQuery,
  useListLegalHoldsQuery,
  useRetentionPreviewQuery
} from 'store/api/complianceApi';
import StatCard from 'components/common/StatCard';
import AuditEventsTab from './AuditEventsTab';
import ErasureRequestsTab from './ErasureRequestsTab';
import LegalHoldsTab from './LegalHoldsTab';
import RetentionTab from './RetentionTab';

// CompliancePage is the operator-facing GDPR/compliance dashboard (ADR-0009):
// a summary KPI row, then tabbed surfaces to review and resolve erasure
// requests, manage legal holds, preview retention cleanup, and read the audit
// trail. Destructive actions are step-up-gated on the backend — the global
// StepUpModal handles the 401 + replay transparently. The active tab is synced
// to the `?tab=` query param so the view is shareable and survives a refresh.

const TABS = [
  {
    key: 'requests',
    labelKey: 'adminCompliance.tabs.erasureRequests',
    icon: faUserSlash
  },
  { key: 'holds', labelKey: 'adminCompliance.tabs.legalHolds', icon: faGavel },
  {
    key: 'retention',
    labelKey: 'adminCompliance.tabs.retention',
    icon: faClockRotateLeft
  },
  {
    key: 'audit',
    labelKey: 'adminCompliance.tabs.auditEvents',
    icon: faClipboardList
  }
] as const;

const CompliancePage = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get('tab') || 'requests';

  const handleTabSelect = (key: string | null) => {
    if (!key) return;
    setSearchParams(
      prev => {
        prev.set('tab', key);
        return prev;
      },
      { replace: true }
    );
  };

  // The summary cards share the same RTK Query caches the tabs read, so there
  // is no extra network cost — RTK Query dedupes the in-flight requests.
  const erasures = useListErasureRequestsQuery();
  const holds = useListLegalHoldsQuery();
  const retention = useRetentionPreviewQuery();
  const audit = useListAuditEventsQuery({ limit: 50 });

  const pendingErasures = erasures.data?.items?.length ?? 0;
  const activeHolds = (holds.data?.items ?? []).filter(h => h.active).length;
  const retentionCandidates = retention.data?.count ?? 0;

  return (
    <>
      <Card className="mb-3">
        <Card.Body className="py-3 px-4">
          {/* h3 = the shared admin page-header genre (see /admin/modules). */}
          <h3 className="mb-1">{t('adminCompliance.pageTitle')}</h3>
          <p className="text-muted mb-0 fs-10">
            {t('adminCompliance.pageSubtitle')}
          </p>
        </Card.Body>
      </Card>

      <Row className="g-3 mb-3">
        <Col md={6} lg={3}>
          <StatCard
            title={t('adminCompliance.stats.pendingErasures.title')}
            value={pendingErasures}
            icon={faUserSlash}
            color="warning"
            accent={pendingErasures > 0 ? 'warning' : undefined}
            subtitle={t('adminCompliance.stats.pendingErasures.subtitle')}
            badge={
              pendingErasures > 0
                ? { text: t('adminCompliance.stats.pendingErasures.badge') }
                : undefined
            }
            loading={erasures.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('adminCompliance.stats.activeHolds.title')}
            value={activeHolds}
            icon={faGavel}
            color="danger"
            accent={activeHolds > 0 ? 'danger' : undefined}
            subtitle={t('adminCompliance.stats.activeHolds.subtitle')}
            badge={
              activeHolds > 0
                ? { text: t('adminCompliance.stats.activeHolds.badge') }
                : undefined
            }
            loading={holds.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('adminCompliance.stats.retentionCandidates.title')}
            value={retentionCandidates}
            icon={faClockRotateLeft}
            color="info"
            accent={retentionCandidates > 0 ? 'info' : undefined}
            subtitle={t('adminCompliance.stats.retentionCandidates.subtitle')}
            badge={
              retentionCandidates > 0
                ? {
                    text: t('adminCompliance.stats.retentionCandidates.badge', {
                      count: retentionCandidates
                    })
                  }
                : undefined
            }
            loading={retention.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <StatCard
            title={t('adminCompliance.stats.auditEvents.title')}
            value={audit.data?.total ?? 0}
            icon={faClipboardList}
            color="primary"
            subtitle={t('adminCompliance.stats.auditEvents.subtitle')}
            loading={audit.isLoading}
          />
        </Col>
      </Row>

      <Tab.Container activeKey={activeTab} onSelect={handleTabSelect}>
        <Nav variant="pills" className="mb-3 flex-wrap gap-2">
          {TABS.map(tab => (
            <Nav.Item key={tab.key}>
              <Nav.Link eventKey={tab.key} className="text-nowrap">
                <FontAwesomeIcon icon={tab.icon} className="me-2" />
                {t(tab.labelKey)}
              </Nav.Link>
            </Nav.Item>
          ))}
        </Nav>
        <Tab.Content>
          <Tab.Pane eventKey="requests">
            <ErasureRequestsTab />
          </Tab.Pane>
          <Tab.Pane eventKey="holds">
            <LegalHoldsTab />
          </Tab.Pane>
          <Tab.Pane eventKey="retention">
            <RetentionTab />
          </Tab.Pane>
          <Tab.Pane eventKey="audit">
            <AuditEventsTab />
          </Tab.Pane>
        </Tab.Content>
      </Tab.Container>
    </>
  );
};

export default CompliancePage;
