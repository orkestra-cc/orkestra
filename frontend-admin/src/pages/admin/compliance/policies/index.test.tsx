import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import PolicyDetailPage from './index';

// useBlocker needs a data router; renderWithProviders uses <MemoryRouter>.
// The predicate is covered by policyNavigation.test.ts.
vi.mock('react-router', async importOriginal => {
  const actual = await importOriginal<typeof import('react-router')>();
  return { ...actual, useBlocker: () => ({ state: 'unblocked' as const }) };
});

const strict = {
  uuid: 'p-1',
  name: 'Strict',
  description: '',
  isPlatformDefault: false,
  version: 1,
  logContent: {
    ipAddress: 'omitted',
    userAgent: 'full',
    subjectIds: 'uuid',
    piiKeys: ['email'],
    scanFreeText: true
  },
  retention: { privileged_change: 400 },
  accountability: {
    role: 'processor',
    ropaRef: 'R-1',
    assessmentRef: 'D-1',
    owner: '',
    reviewDueAt: '2027-03-30T00:00:00Z'
  },
  createdBy: 'alice',
  updatedBy: 'alice',
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  changeReason: 'x'
};
const platform = {
  ...strict,
  uuid: 'plat',
  name: 'Platform',
  isPlatformDefault: true,
  retention: { admin_access: 365, privileged_change: 730 },
  assignedTenants: 0,
  lessRestrictiveFields: [],
  reviewOverdue: false
};
const classes = [
  'admin_access',
  'privileged_change',
  'client_activity',
  'authentication_security',
  'compliance_evidence'
].map((key, i) => ({
  key,
  events: '',
  purpose: '',
  legalBasis: '',
  minDays: key === 'admin_access' ? 184 : 1,
  hardMinimum: key === 'admin_access',
  defaultDays: 365,
  tenantSettable: i < 3
}));

const stub = (opts: { validate?: object; put?: () => Response } = {}) => {
  const seen: { put?: unknown } = {};
  server.use(
    http.get(url('/v1/admin/compliance/policies/p-1'), () =>
      HttpResponse.json({ policy: strict, assignments: [] })
    ),
    http.get(url('/v1/admin/compliance/policies'), () =>
      HttpResponse.json({ items: [platform] })
    ),
    http.get(url('/v1/admin/compliance/retention-classes'), () =>
      HttpResponse.json({ items: classes })
    ),
    http.post(url('/v1/admin/compliance/policies/validate'), () =>
      HttpResponse.json(
        opts.validate ?? {
          errors: [],
          warnings: [{ code: 'ip_full', field: 'logContent.ipAddress' }]
        }
      )
    ),
    http.put(url('/v1/admin/compliance/policies/p-1'), async ({ request }) => {
      seen.put = await request.json();
      return opts.put
        ? opts.put()
        : HttpResponse.json(
            { applied: false, warnings: [], changeRequest: { uuid: 'cr-1' } },
            { status: 202 }
          );
    })
  );
  return seen;
};

const renderPage = (entry = '/admin/compliance/policies/p-1') =>
  renderWithProviders(
    <Routes>
      <Route
        path="/admin/compliance/policies/:policyId"
        element={<PolicyDetailPage />}
      />
      <Route path="/admin/compliance" element={<div>compliance home</div>} />
    </Routes>,
    { routerEntries: [entry] }
  );

describe('PolicyDetailPage', () => {
  it('validates, shows the warnings and sends the change with its reason and acknowledgement', async () => {
    const seen = stub();
    renderPage();
    const user = userEvent.setup();
    expect(await screen.findByDisplayValue('Strict')).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText('IP addresses'), 'full');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(
      await screen.findByText('Full IP addresses will reach the logs.')
    ).toBeInTheDocument();
    await user.type(screen.getByLabelText('Reason'), 'richiesta del cliente');
    await user.click(
      screen.getByLabelText('I have read the warnings and confirm the change')
    );
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(seen.put).toBeDefined());
    expect(seen.put).toMatchObject({
      expectedVersion: 1,
      reason: 'richiesta del cliente',
      acknowledgeWarnings: true,
      policy: { logContent: { ipAddress: 'full' } }
    });
  });

  it('refuses to confirm warnings that are not acknowledged', async () => {
    const seen = stub();
    renderPage();
    const user = userEvent.setup();
    await user.selectOptions(
      await screen.findByLabelText('IP addresses'),
      'full'
    );
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await user.type(await screen.findByLabelText('Reason'), 'motivo');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(
      await screen.findByText('Confirm the warnings to continue.')
    ).toBeInTheDocument();
    expect(seen.put).toBeUndefined();
  });

  it('puts a validation error on its field and opens no confirmation', async () => {
    stub({
      validate: {
        errors: [
          {
            code: 'class_below_minimum',
            field: 'retention.admin_access',
            params: { min: 184 }
          }
        ],
        warnings: []
      }
    });
    renderPage();
    const user = userEvent.setup();
    await user.type(
      await screen.findByLabelText('Administrator access'),
      '100'
    );
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(/At least 184 days/)).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'Confirm' })
    ).not.toBeInTheDocument();
  });

  it('locks the editor after a version conflict', async () => {
    stub({
      put: () =>
        HttpResponse.json(
          {
            status: 409,
            detail: 'changed',
            code: 'compliance.policy_version_conflict'
          },
          { status: 409 }
        ) as unknown as Response
    });
    renderPage();
    const user = userEvent.setup();
    await user.selectOptions(
      await screen.findByLabelText('IP addresses'),
      'full'
    );
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await user.type(await screen.findByLabelText('Reason'), 'motivo');
    await user.click(
      screen.getByLabelText('I have read the warnings and confirm the change')
    );
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(await screen.findByText('The policy changed')).toBeInTheDocument();
    expect(screen.getByLabelText('IP addresses')).toBeDisabled();
  });

  it('creates a new tenant policy from the platform content and opens it', async () => {
    stub({ validate: { errors: [], warnings: [] } });
    const seen: { post?: unknown } = {};
    server.use(
      http.post(url('/v1/admin/compliance/policies'), async ({ request }) => {
        seen.post = await request.json();
        return HttpResponse.json({
          applied: true,
          policy: strict,
          warnings: []
        });
      })
    );
    renderPage('/admin/compliance/policies/new');
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText('Name'), 'Clinica');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await user.type(await screen.findByLabelText('Reason'), 'nuovo cliente');
    await user.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(seen.post).toBeDefined());
    expect(seen.post).toMatchObject({
      reason: 'nuovo cliente',
      acknowledgeWarnings: false,
      policy: { name: 'Clinica', retention: {} }
    });
    expect(await screen.findByDisplayValue('Strict')).toBeInTheDocument();
  });
});
