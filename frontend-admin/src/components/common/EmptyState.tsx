import { Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

// EmptyState is the friendly zero-row placeholder of a list surface: a
// centered icon + message block, with an optional hint and an optional
// call-to-action button (first used by /admin/compliance, then /admin/llm).
interface EmptyStateProps {
  icon: IconProp;
  message: string;
  hint?: string;
  ctaLabel?: string;
  onCta?: () => void;
  ctaDisabled?: boolean;
}

const EmptyState = ({
  icon,
  message,
  hint,
  ctaLabel,
  onCta,
  ctaDisabled
}: EmptyStateProps) => (
  <div className="text-center text-muted py-5">
    <FontAwesomeIcon icon={icon} className="fs-5 text-300 mb-3" />
    <p className="mb-0 fw-semibold text-600">{message}</p>
    {/* text-600, not text-500: the hint is text and 2.6:1 misses AA — the
        journey must not end on a barely-readable note. */}
    {hint && <p className="mb-0 fs-11 text-600 mt-1">{hint}</p>}
    {ctaLabel && onCta && (
      <Button
        variant="orkestra-primary"
        size="sm"
        className="mt-3"
        disabled={ctaDisabled}
        onClick={onCta}
      >
        {ctaLabel}
      </Button>
    )}
  </div>
);

export default EmptyState;
