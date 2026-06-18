import { BadgeColor } from 'components/common/SubtleBadge';

// Shared formatting helpers for the compliance tabs.

export const formatDateTime = (value?: string): string =>
  value ? new Date(value).toLocaleString() : '—';

// Audit outcomes map to the same traffic-light palette everywhere they render.
export const outcomeColor = (outcome: string): BadgeColor => {
  switch (outcome) {
    case 'success':
      return 'success';
    case 'denied':
      return 'warning';
    default:
      return 'danger';
  }
};

// Erasure-request lifecycle status → badge color.
export const erasureStatusColor = (status: string): BadgeColor => {
  switch (status) {
    case 'completed':
    case 'executed':
      return 'success';
    case 'rejected':
      return 'secondary';
    case 'pending':
      return 'warning';
    default:
      return 'info';
  }
};
