// What the console says when an operator's email (password reset,
// verification, invite) was not sent because the address is on the email
// suppression list: the backend answers 409 auth.recipient_suppressed and
// mints nothing.
//
// The core does not know which module lets an operator make an address valid
// again, so it publishes a slot: a module that manages the suppression list
// contributes a component (typically a link to its page), rendered under the
// message. Without a contribution the message stands alone.

import { Suspense, type ReactNode } from 'react';
import { useSlotContributions } from 'modules/useSlotContributions';
import type { ResolvedSlotContribution } from 'modules/slotContributions';

/** Slot id: «where an address on the suppression list is made valid again». */
export const SUPPRESSION_MANAGE_SLOT = 'notification.suppression.manage';

/** Props every contribution to SUPPRESSION_MANAGE_SLOT receives. */
export interface SuppressionManageProps {
  /** The address that was refused, when the caller knows it. */
  address?: string;
}

/** The error code of a send refused because the address is suppressed. */
export const RECIPIENT_SUPPRESSED = 'auth.recipient_suppressed';

/** True when an RTK Query error carries auth.recipient_suppressed. */
export function isRecipientSuppressed(err: unknown): boolean {
  if (!err || typeof err !== 'object' || !('data' in err)) return false;
  const data = (err as { data?: { code?: string } }).data;
  return data?.code === RECIPIENT_SUPPRESSED;
}

/** The contributions to SUPPRESSION_MANAGE_SLOT the caller may see. */
export function useSuppressionManageLinks(): ResolvedSlotContribution[] {
  return useSlotContributions(SUPPRESSION_MANAGE_SLOT);
}

interface SuppressedSendMessageProps extends SuppressionManageProps {
  message: string;
  links: ResolvedSlotContribution[];
}

/** The toast body: the message, then each contribution on its own line. */
export const SuppressedSendMessage = ({
  message,
  links,
  address
}: SuppressedSendMessageProps) => (
  <div>
    <div>{message}</div>
    {links.map(({ key, component: Contribution }) => (
      <div key={key} className="mt-1">
        <Suspense fallback={null}>
          <Contribution address={address} />
        </Suspense>
      </div>
    ))}
  </div>
);

/**
 * The toast content for a failed send: the message alone, or the message
 * with the slot's contributions when the address is suppressed.
 */
export function suppressedSendToast(
  err: unknown,
  message: string,
  links: ResolvedSlotContribution[],
  address?: string
): ReactNode {
  if (!isRecipientSuppressed(err) || links.length === 0) return message;
  return (
    <SuppressedSendMessage message={message} links={links} address={address} />
  );
}
