import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import CompliancePolicyCard from './CompliancePolicyCard';

const strict = {
  uuid: 'p-1',
  name: 'Strict',
  description: '',
  isPlatformDefault: false,
  version: 2,
  logContent: {
    ipAddress: 'omitted',
    userAgent: 'full',
    subjectIds: 'uuid',
    piiKeys: [],
    scanFreeText: true
  },
  retention: { privileged_change: 400 },
  accountability: {
    role: 'processor',
    ropaRef: 'R',
    assessmentRef: 'D',
    owner: ''
  },
  createdBy: 'a',
  updatedBy: 'a',
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  changeReason: 'x'
};

const stub = () => {
  const seen: { unassign?: unknown } = {};
  server.use(
    http.get(url('/v1/admin/compliance/policies/effective'), () =>
      HttpResponse.json({
        tenantId: 't-1',
        source: 'assigned',
        policy: strict,
        retention: [
          {
            class: 'admin_access',
            policyUuid: 'plat',
            policyVersion: 1,
            days: 365
          },
          {
            class: 'privileged_change',
            policyUuid: 'p-1',
            policyVersion: 2,
            days: 400
          }
        ]
      })
    ),
    http.get(url('/v1/admin/compliance/policies'), () =>
      HttpResponse.json({
        items: [
          {
            ...strict,
            assignedTenants: 1,
            lessRestrictiveFields: [],
            reviewOverdue: false
          }
        ]
      })
    ),
    http.post(url('/v1/admin/compliance/policy-assignments/t-1/validate'), () =>
      HttpResponse.json({
        errors: [],
        warnings: [
          {
            code: 'less_restrictive_than_current',
            params: { fields: ['logContent.ipAddress'] }
          }
        ]
      })
    ),
    http.delete(
      url('/v1/admin/compliance/policy-assignments/t-1'),
      async ({ request }) => {
        seen.unassign = await request.json();
        return HttpResponse.json(
          { applied: false, warnings: [], changeRequest: { uuid: 'cr-1' } },
          { status: 202 }
        );
      }
    )
  );
  return seen;
};

describe('CompliancePolicyCard', () => {
  it('shows the effective policy, where it comes from and the retention by class', async () => {
    stub();
    renderWithProviders(<CompliancePolicyCard tenantId="t-1" />);
    expect(await screen.findByText('Strict')).toBeInTheDocument();
    expect(screen.getByText('Assigned')).toBeInTheDocument();
    expect(screen.getByText('Removed')).toBeInTheDocument(); // IP omitted
    expect(screen.getByText('400 days')).toBeInTheDocument();
  });

  it('moves the tenant back to the platform with the warnings acknowledged', async () => {
    const seen = stub();
    renderWithProviders(<CompliancePolicyCard tenantId="t-1" />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Change' }));
    await user.selectOptions(screen.getByLabelText('Policy'), '');
    expect(
      await screen.findByText(/Weakens the current protection: IP addresses/)
    ).toBeInTheDocument();
    await user.type(screen.getByLabelText('Reason'), 'fine del contratto');
    await user.click(
      screen.getByLabelText('I have read the warnings and confirm the change')
    );
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(seen.unassign).toEqual({
        reason: 'fine del contratto',
        acknowledgeWarnings: true
      })
    );
  });
});
