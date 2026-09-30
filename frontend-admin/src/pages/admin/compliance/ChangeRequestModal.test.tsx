import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, within } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { ChangeRequest } from 'store/api/complianceApi';
import ChangeRequestModal from './ChangeRequestModal';

// An update request: the diff needs the current policy, so the approver
// must not see (nor approve) a diff computed against nothing.
const updateRequest: ChangeRequest = {
  uuid: 'cr-up',
  kind: 'update',
  policyUuid: 'pol-1',
  payload: {
    policy: {
      name: 'Strict',
      description: '',
      logContent: {
        ipAddress: 'full',
        userAgent: 'full',
        subjectIds: 'uuid',
        piiKeys: [],
        scanFreeText: true
      },
      retention: { admin_access: 365 },
      accountability: {
        role: 'processor',
        ropaRef: 'RoPA-1',
        assessmentRef: 'DPIA-1',
        owner: ''
      }
    }
  },
  expectedVersion: 1,
  warnings: ['ip_full'],
  reason: 'richiesta del cliente',
  requestedBy: 'someone-else',
  requestedAt: '2026-09-29T10:00:00Z',
  status: 'pending'
};

const renderModal = () => {
  renderWithProviders(
    <ChangeRequestModal request={updateRequest} onHide={() => {}} />
  );
  return within(screen.getByRole('dialog'));
};

describe('ChangeRequestModal — update requests', () => {
  it('keeps Approve disabled and shows no diff while the current policy loads', async () => {
    server.use(
      http.get(
        url('/v1/admin/compliance/policies/pol-1'),
        () => new Promise<never>(() => {})
      )
    );
    const dialog = renderModal();

    expect(await dialog.findByRole('status')).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Approve' })).toBeDisabled();
    expect(dialog.getByRole('button', { name: 'Reject' })).toBeEnabled();
    expect(dialog.queryByText('Changes')).not.toBeInTheDocument();
  });

  it('shows an error and keeps Approve disabled when the current policy cannot be loaded', async () => {
    server.use(
      http.get(url('/v1/admin/compliance/policies/pol-1'), () =>
        HttpResponse.json(
          { status: 500, title: 'Internal Server Error' },
          { status: 500 }
        )
      )
    );
    const dialog = renderModal();

    expect(
      await dialog.findByText(
        'The current version of the policy could not be loaded, so the changes cannot be shown. Reject the request or try again later.'
      )
    ).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Approve' })).toBeDisabled();
    expect(dialog.getByRole('button', { name: 'Reject' })).toBeEnabled();
    expect(dialog.queryByText('Changes')).not.toBeInTheDocument();
  });
});
