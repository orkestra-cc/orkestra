import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Provider } from 'react-redux';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { setupStore } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import PolicyDetailPage from './index';

// Unlike index.test.tsx, useBlocker is NOT mocked here: it needs a data
// router, so the page is rendered under createMemoryRouter + RouterProvider
// and the unsaved-changes guard is exercised end to end.

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

const renderPage = () => {
  server.use(
    http.get(url('/v1/admin/compliance/policies/p-1/versions'), () =>
      HttpResponse.json({ items: [] })
    ),
    http.get(url('/v1/admin/compliance/policies/p-1'), () =>
      HttpResponse.json({ policy: strict, assignments: [] })
    ),
    http.get(url('/v1/admin/compliance/policies'), () =>
      HttpResponse.json({ items: [platform] })
    ),
    http.get(url('/v1/admin/compliance/retention-classes'), () =>
      HttpResponse.json({ items: classes })
    )
  );
  const router = createMemoryRouter(
    [
      {
        path: '/admin/compliance/policies/:policyId',
        element: <PolicyDetailPage />
      },
      { path: '/admin/compliance', element: <div>compliance home</div> }
    ],
    { initialEntries: ['/admin/compliance/policies/p-1'] }
  );
  render(
    <Provider store={setupStore()}>
      <RouterProvider router={router} />
    </Provider>
  );
  return router;
};

const makeDirty = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.selectOptions(
    await screen.findByLabelText('IP addresses'),
    'full'
  );
};

describe('PolicyDetailPage unsaved-changes guard', () => {
  it('asks before leaving the page with unsaved changes, and Stay keeps the page and the edit', async () => {
    const router = renderPage();
    const user = userEvent.setup();
    await makeDirty(user);
    await user.click(screen.getByRole('link', { name: 'Compliance' }));
    expect(await screen.findByText('Unsaved changes')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Stay' }));
    expect(router.state.location.pathname).toBe(
      '/admin/compliance/policies/p-1'
    );
    expect(screen.queryByText('compliance home')).not.toBeInTheDocument();
    expect(screen.getByLabelText('IP addresses')).toHaveValue('full');
  });

  it('leaves the page once the operator discards the changes', async () => {
    const router = renderPage();
    const user = userEvent.setup();
    await makeDirty(user);
    await user.click(screen.getByRole('link', { name: 'Compliance' }));
    await user.click(
      await screen.findByRole('button', { name: 'Discard and leave' })
    );
    expect(await screen.findByText('compliance home')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin/compliance');
  });

  it('does not ask when switching section, and keeps the edit', async () => {
    const router = renderPage();
    const user = userEvent.setup();
    await makeDirty(user);
    await user.click(screen.getByText('History'));
    expect(router.state.location.search).toBe('?section=history');
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument();

    await user.click(screen.getByText('Settings'));
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument();
    expect(screen.getByLabelText('IP addresses')).toHaveValue('full');
  });

  it('does not ask when the form is clean', async () => {
    renderPage();
    const user = userEvent.setup();
    await screen.findByLabelText('IP addresses');
    await user.click(screen.getByRole('link', { name: 'Compliance' }));
    expect(await screen.findByText('compliance home')).toBeInTheDocument();
  });
});
