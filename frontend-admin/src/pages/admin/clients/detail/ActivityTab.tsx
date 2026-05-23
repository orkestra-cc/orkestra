import { Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans } from 'react-i18next';

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
    <Trans
      i18nKey="adminClients.activity.placeholder"
      components={{ code: <code /> }}
    />
  </Alert>
);

export default ActivityTab;
