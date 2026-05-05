import { Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

/**
 * Activity tab stub. A unified tenant_activity log that aggregates
 * subscription, payment, membership, and impersonation events is a
 * follow-up; the compliance audit sink today records per-module events
 * scattered across their own collections. Rendered as an informative
 * placeholder so the tab slot reads cleanly instead of leaving a
 * dead-end in the UX.
 */
const ActivityTab: React.FC = () => (
  <Alert variant="light" className="fs-10 py-4 border text-center">
    <FontAwesomeIcon icon="clock" className="text-info me-2" />
    Per-tenant activity log lands in a follow-up. Until then, module-level
    audit trails (<code>compliance_audit_events</code>,{' '}
    <code>subscriptions_activity</code>) carry the per-resource history.
  </Alert>
);

export default ActivityTab;
