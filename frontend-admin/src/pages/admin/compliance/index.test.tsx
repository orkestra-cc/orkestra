import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import CompliancePage from './index';

// CompliancePage mounts four RTK Query tabs at once (react-bootstrap Tabs
// renders every pane), so each test stubs all four read endpoints to keep
// MSW's onUnhandledRequest:'error' guard happy.

const erasureRequests = {
  items: [
    {
      uuid: 'er-1',
      userUuid: 'u-erase-1',
      reason: 'No longer a customer',
      status: 'pending',
      requestedAt: '2026-06-01T10:00:00Z'
    }
  ]
};
const legalHolds = {
  items: [
    {
      uuid: 'lh-1',
      userUuid: 'u-hold-1',
      reason: 'Litigation hold',
      placedBy: 'admin-1',
      placedAt: '2026-06-01T09:00:00Z',
      active: true
    }
  ]
};
const retentionPreview = {
  cutoff: '2021-06-01T00:00:00Z',
  count: 2,
  userUuids: ['u-old-1', 'u-old-2']
};
const auditEvents = {
  items: [
    {
      uuid: 'ae-1',
      actorType: 'user',
      action: 'auth.login.succeeded',
      outcome: 'success',
      timestamp: '2026-06-01T08:00:00Z'
    }
  ],
  total: 1,
  limit: 50,
  offset: 0
};

const stubReads = (overrides?: {
  erasure?: typeof erasureRequests;
  holds?: typeof legalHolds;
  retention?: typeof retentionPreview;
  audit?: typeof auditEvents;
}) => {
  server.use(
    http.get(url('/v1/admin/compliance/erasure-requests'), () =>
      HttpResponse.json(overrides?.erasure ?? erasureRequests)
    ),
    http.get(url('/v1/admin/compliance/legal-holds'), () =>
      HttpResponse.json(overrides?.holds ?? legalHolds)
    ),
    http.get(url('/v1/admin/compliance/retention/preview'), () =>
      HttpResponse.json(overrides?.retention ?? retentionPreview)
    ),
    http.get(url('/v1/admin/audit-events'), () =>
      HttpResponse.json(overrides?.audit ?? auditEvents)
    )
  );
};

describe('CompliancePage', () => {
  it('renders the data from every tab endpoint', async () => {
    stubReads();
    renderWithProviders(<CompliancePage />);

    // Default (erasure requests) tab.
    expect(await screen.findByText('u-erase-1')).toBeInTheDocument();
    // The other panes are mounted too, so their data is present.
    expect(await screen.findByText('Litigation hold')).toBeInTheDocument();
    expect(await screen.findByText('auth.login.succeeded')).toBeInTheDocument();
    // Retention preview lists the subjects past the retention window.
    expect(await screen.findByText('u-old-1')).toBeInTheDocument();
  });

  it('shows empty states when every endpoint returns nothing', async () => {
    stubReads({
      erasure: { items: [] },
      holds: { items: [] },
      retention: { cutoff: '2021-06-01T00:00:00Z', count: 0, userUuids: [] },
      audit: { items: [], total: 0, limit: 50, offset: 0 }
    });
    renderWithProviders(<CompliancePage />);

    expect(
      await screen.findByText(/no pending erasure requests/i)
    ).toBeInTheDocument();
    expect(
      await screen.findByText(/no active legal holds/i)
    ).toBeInTheDocument();
    expect(await screen.findByText(/no audit events/i)).toBeInTheDocument();
  });

  // The global filter matches cell VALUES, not rendered text, so a date column
  // accessored on the raw ISO string is searchable only by a string the
  // operator never sees. Every ComplianceTable ships a search box, so this was
  // live here, not latent.
  //
  // Search on the MONTH NAME, not the time: the runner is UTC, where a rendered
  // "10:00" is byte-identical to the ISO "10:00" behind it and the bug is
  // invisible. A month name never appears in an ISO string, in any locale.
  it('filters legal holds by the date text the operator can see', async () => {
    stubReads({
      holds: {
        items: [
          { ...legalHolds.items[0], placedAt: '2026-06-01T09:00:00Z' },
          {
            ...legalHolds.items[0],
            uuid: 'lh-2',
            userUuid: 'u-hold-2',
            reason: 'Second hold',
            placedAt: '2026-11-02T09:00:00Z'
          }
        ]
      }
    });
    renderWithProviders(<CompliancePage />);
    const user = userEvent.setup();

    const firstRow = (await screen.findByText('u-hold-1')).closest('tr')!;
    const placedCell = [...firstRow.querySelectorAll('td')].find(td =>
      /[A-Za-z]{3,}\s*\d/.test(td.textContent ?? '')
    )!;
    const month = placedCell.textContent!.match(/[A-Za-z]{3,}/)![0];

    await user.type(
      screen.getByPlaceholderText(/search by subject, reason or case/i),
      month
    );

    await waitFor(() => {
      expect(screen.queryByText('u-hold-2')).not.toBeInTheDocument();
    });
    expect(screen.getByText('u-hold-1')).toBeInTheDocument();
  });

  it('sorts legal holds chronologically, not lexicographically', async () => {
    // Sep 2026 is OLDER than Jan 2027 but sorts AFTER it as text ("J" < "S"),
    // so the two orders disagree and a formatted-string comparator flips them.
    stubReads({
      holds: {
        items: [
          {
            ...legalHolds.items[0],
            uuid: 'lh-sep',
            userUuid: 'u-older',
            placedAt: '2026-09-05T10:00:00Z'
          },
          {
            ...legalHolds.items[0],
            uuid: 'lh-jan',
            userUuid: 'u-newer',
            placedAt: '2027-01-05T10:00:00Z'
          }
        ]
      }
    });
    renderWithProviders(<CompliancePage />);
    const user = userEvent.setup();

    await screen.findByText('u-older');
    await user.click(screen.getByText(/^placed$/i));

    await waitFor(() => {
      // All four panes are mounted at once, so scope to the holds table
      // rather than sweeping every tbody on the page.
      const holdsBody = screen.getByText('u-older').closest('tbody')!;
      const subjects = [...holdsBody.querySelectorAll('tr')].map(r =>
        r.querySelector('td')!.textContent!.trim()
      );
      expect(subjects).toEqual(['u-older', 'u-newer']);
    });
  });

  it('executes an erasure request via the execute endpoint', async () => {
    stubReads();
    let executeHit: { path: string; body: unknown } | null = null;
    server.use(
      http.post(
        url('/v1/admin/compliance/erasure-requests/:id/execute'),
        async ({ request }) => {
          executeHit = {
            path: new URL(request.url).pathname,
            body: await request.json()
          };
          return HttpResponse.json({ purged: {} });
        }
      )
    );
    renderWithProviders(<CompliancePage />);

    await screen.findByText('u-erase-1');
    await userEvent.click(screen.getByRole('button', { name: /^execute$/i }));

    await waitFor(() => expect(executeHit).not.toBeNull());
    expect(executeHit!.path).toBe(
      '/v1/admin/compliance/erasure-requests/er-1/execute'
    );
    expect(executeHit!.body).toEqual({ mode: 'hard_delete' });
  });
});
