import { describe, expect, it } from 'vitest';
import { delay, http, HttpResponse } from 'msw';
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

// Three policies so the operator can move between two of them; the list is
// answered after a delay to reproduce a cold cache on first open.
const stubTwoPolicies = (listDelayMs = 0) => {
  const seen: { assign: unknown[]; validated: unknown[] } = {
    assign: [],
    validated: []
  };
  const item = (uuid: string, name: string) => ({
    ...strict,
    uuid,
    name,
    assignedTenants: 0,
    lessRestrictiveFields: [],
    reviewOverdue: false
  });
  server.use(
    http.get(url('/v1/admin/compliance/policies/effective'), () =>
      HttpResponse.json({
        tenantId: 't-1',
        source: 'assigned',
        policy: strict,
        retention: []
      })
    ),
    http.get(url('/v1/admin/compliance/policies'), async () => {
      await delay(listDelayMs);
      return HttpResponse.json({
        items: [item('p-1', 'Strict'), item('a', 'Alpha'), item('b', 'Beta')]
      });
    }),
    http.post(
      url('/v1/admin/compliance/policy-assignments/t-1/validate'),
      async ({ request }) => {
        seen.validated.push(await request.json());
        return HttpResponse.json({
          errors: [],
          warnings: [
            {
              code: 'less_restrictive_than_current',
              params: { fields: ['logContent.ipAddress'] }
            }
          ]
        });
      }
    ),
    http.put(
      url('/v1/admin/compliance/policy-assignments/t-1'),
      async ({ request }) => {
        seen.assign.push(await request.json());
        return HttpResponse.json({ applied: true, warnings: [] });
      }
    )
  );
  return seen;
};

describe('AssignPolicyModal', () => {
  it('never carries an acknowledgement over to another policy', async () => {
    const seen = stubTwoPolicies();
    renderWithProviders(<CompliancePolicyCard tenantId="t-1" />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Change' }));
    const select = screen.getByLabelText('Policy');
    await waitFor(() =>
      expect(screen.getByRole('option', { name: 'Alpha' })).toBeInTheDocument()
    );

    await user.selectOptions(select, 'a');
    const ackLabel = 'I have read the warnings and confirm the change';
    await user.click(await screen.findByLabelText(ackLabel));
    expect(screen.getByLabelText(ackLabel)).toBeChecked();

    await user.selectOptions(select, 'b');
    await waitFor(() =>
      expect(seen.validated).toContainEqual({ policyId: 'b' })
    );
    const box = await screen.findByLabelText(ackLabel);
    expect(box).not.toBeChecked();

    await user.type(screen.getByLabelText('Reason'), 'cambio');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(box).toHaveClass('is-invalid'));
    expect(seen.assign).toEqual([]);
  });

  it('shows the current policy selected even when the list arrives late', async () => {
    const seen = stubTwoPolicies(150);
    renderWithProviders(<CompliancePolicyCard tenantId="t-1" />);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Change' }));
    await waitFor(() =>
      expect(screen.getByRole('option', { name: 'Alpha' })).toBeInTheDocument()
    );
    const select = screen.getByLabelText('Policy');
    expect(select).toHaveValue('p-1');

    await user.selectOptions(select, '');
    await waitFor(() => expect(seen.validated).toHaveLength(1));
  });
});
