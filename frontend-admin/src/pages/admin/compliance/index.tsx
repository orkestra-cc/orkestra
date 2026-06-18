import { Col, Nav, Row, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faClipboardList,
  faClockRotateLeft,
  faGavel,
  faShieldHalved,
  faUserSlash
} from '@fortawesome/free-solid-svg-icons';
import { useSearchParams } from 'react-router-dom';
import {
  useListAuditEventsQuery,
  useListErasureRequestsQuery,
  useListLegalHoldsQuery,
  useRetentionPreviewQuery
} from 'store/api/complianceApi';
import AuditEventsTab from './AuditEventsTab';
import ComplianceStatCard from './ComplianceStatCard';
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
  { key: 'requests', label: 'Erasure Requests', icon: faUserSlash },
  { key: 'holds', label: 'Legal Holds', icon: faGavel },
  { key: 'retention', label: 'Retention', icon: faClockRotateLeft },
  { key: 'audit', label: 'Audit Events', icon: faClipboardList }
] as const;

const CompliancePage = () => {
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
      <div className="mb-3">
        <h2 className="mb-1">
          <FontAwesomeIcon
            icon={faShieldHalved}
            className="me-2 text-primary"
          />
          Compliance
        </h2>
        <p className="text-muted mb-0">
          Audit trail &amp; GDPR data-subject rights — review erasure requests,
          manage legal holds, preview retention cleanup, and read the audit log.
        </p>
      </div>

      <Row className="g-3 mb-3">
        <Col md={6} lg={3}>
          <ComplianceStatCard
            title="Pending Erasures"
            value={pendingErasures}
            icon={faUserSlash}
            color="warning"
            subtitle="Awaiting review"
            badgeText={pendingErasures > 0 ? 'Needs attention' : undefined}
            loading={erasures.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <ComplianceStatCard
            title="Active Legal Holds"
            value={activeHolds}
            icon={faGavel}
            color="danger"
            subtitle="Blocking erasure"
            badgeText={activeHolds > 0 ? 'Erasure blocked' : undefined}
            loading={holds.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <ComplianceStatCard
            title="Retention Candidates"
            value={retentionCandidates}
            icon={faClockRotateLeft}
            color="info"
            subtitle="Past retention window"
            badgeText={
              retentionCandidates > 0
                ? `${retentionCandidates} subjects`
                : undefined
            }
            loading={retention.isLoading}
          />
        </Col>
        <Col md={6} lg={3}>
          <ComplianceStatCard
            title="Audit Events"
            value={audit.data?.total ?? 0}
            icon={faClipboardList}
            color="primary"
            subtitle="Recorded total"
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
                {tab.label}
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
