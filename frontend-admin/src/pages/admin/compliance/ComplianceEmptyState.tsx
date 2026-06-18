import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// ComplianceEmptyState is the friendly zero-row placeholder shared by the tabs,
// replacing the previous bare one-liner with a centered icon + message block.
interface ComplianceEmptyStateProps {
  icon: IconProp;
  message: string;
  hint?: string;
}

const ComplianceEmptyState = ({
  icon,
  message,
  hint
}: ComplianceEmptyStateProps) => (
  <div className="text-center text-muted py-5">
    <FontAwesomeIcon icon={icon} className="fs-5 text-300 mb-3" />
    <p className="mb-0 fw-semibold text-600">{message}</p>
    {hint && <p className="mb-0 fs-11 text-500 mt-1">{hint}</p>}
  </div>
);

export default ComplianceEmptyState;
