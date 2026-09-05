import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// SecurityEmptyState is the zero-row placeholder shared by the security tabs —
// the same centered icon + message + hint block /admin/compliance uses, in
// place of the bare left-aligned sentence these panes used to render.
interface SecurityEmptyStateProps {
  icon: IconProp;
  message: string;
  hint?: string;
}

const SecurityEmptyState = ({
  icon,
  message,
  hint
}: SecurityEmptyStateProps) => (
  <div className="text-center text-muted py-5">
    <FontAwesomeIcon icon={icon} className="fs-5 text-300 mb-3" />
    <p className="mb-0 fw-semibold text-600">{message}</p>
    {/* text-600, not text-500: the hint is text and owes 4.5:1. */}
    {hint && <p className="mb-0 fs-11 text-600 mt-1">{hint}</p>}
  </div>
);

export default SecurityEmptyState;
