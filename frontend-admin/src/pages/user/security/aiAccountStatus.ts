import type { BadgeColor } from 'components/common/SubtleBadge';
import type { LlmAccountStatus } from 'store/api/llmApi';

// The badge colour of a linked ChatGPT account's status, shared by the
// user's AI accounts tab and the operators' /admin/llm accounts tab.
export const LLM_ACCOUNT_STATUS_BADGE: Record<LlmAccountStatus, BadgeColor> = {
  active: 'success',
  plan_disabled: 'warning',
  needs_reauth: 'danger',
  revoked: 'secondary'
};
