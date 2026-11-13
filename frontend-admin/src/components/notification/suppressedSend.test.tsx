import { lazy } from 'react';
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { ResolvedSlotContribution } from 'modules/slotContributions';
import {
  isRecipientSuppressed,
  suppressedSendToast,
  type SuppressionManageProps
} from './suppressedSend';

const Where = ({ address }: SuppressionManageProps) => (
  <a href={`/manage?q=${address}`}>manage {address}</a>
);
const links: ResolvedSlotContribution[] = [
  {
    key: 'widgets.manage',
    labelKey: 'widgets.manage',
    moduleName: 'widgets',
    component: lazy(async () => ({ default: Where }))
  }
];
const suppressed = { status: 409, data: { code: 'auth.recipient_suppressed' } };

describe('suppressedSendToast', () => {
  it('recognises only auth.recipient_suppressed', () => {
    expect(isRecipientSuppressed(suppressed)).toBe(true);
    expect(isRecipientSuppressed({ data: { code: 'auth.other' } })).toBe(false);
    expect(isRecipientSuppressed(new Error('x'))).toBe(false);
    expect(isRecipientSuppressed(undefined)).toBe(false);
  });

  it('is the plain message for any other error, or with no contribution', () => {
    expect(suppressedSendToast({ data: { code: 'x' } }, 'msg', links)).toBe(
      'msg'
    );
    expect(suppressedSendToast(suppressed, 'msg', [])).toBe('msg');
  });

  it('adds each contribution, given the refused address', async () => {
    renderWithProviders(
      <>{suppressedSendToast(suppressed, 'not sent', links, 'a@b.test')}</>
    );
    expect(screen.getByText('not sent')).toBeInTheDocument();
    const link = await screen.findByRole('link', { name: 'manage a@b.test' });
    expect(link).toHaveAttribute('href', '/manage?q=a@b.test');
  });
});
