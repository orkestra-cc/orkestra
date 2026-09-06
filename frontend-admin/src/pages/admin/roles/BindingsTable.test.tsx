import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import type { Binding } from 'store/api/tenantApi';
import BindingsTable from './BindingsTable';

const TENANT = 'org-1';

const binding = (over: Partial<Binding>): Binding => ({
  id: 'b-1',
  userUUID: 'u-1',
  tenantId: TENANT,
  roleId: 'r-1',
  roleName: 'administrator',
  grantedAt: '2026-06-01T10:00:00Z',
  ...over
});

const stubBindings = (bindings: Binding[]) =>
  server.use(
    http.get(url(`/v1/tenants/${TENANT}/authz/bindings`), () =>
      HttpResponse.json({ bindings })
    )
  );

describe('BindingsTable date columns', () => {
  it('renders dates through the console formatting layer, not toLocaleString', async () => {
    stubBindings([binding({ grantedAt: '2026-06-01T10:00:00Z' })]);
    renderWithProviders(<BindingsTable tenantId={TENANT} />);

    // Columns: user, role, granted, expires, actions.
    const row = (await screen.findByText('u-1')).closest('tr')!;
    const granted = row.querySelectorAll('td')[2].textContent!;

    // helpers/dateFormat renders a month NAME and no seconds ("Jun 01, 2026,
    // 10:00"). The raw `new Date(...).toLocaleString()` this column used to
    // call gives "6/1/2026, 10:00:00 AM" — all digits, with a seconds field.
    expect(granted).toMatch(/[A-Za-z]{3,}/);
    expect(granted).not.toMatch(/\d{1,2}\/\d{1,2}\/\d{4}/);
    expect(granted).not.toMatch(/\d{1,2}:\d{2}:\d{2}/);
  });

  it('sorts BOTH date columns chronologically, not lexicographically', async () => {
    // Sep 2026 is OLDER than Jan 2027 but sorts AFTER it as text ("J" < "S"),
    // so the two orders disagree and a formatted-string comparator flips them.
    // Fed NEWEST-FIRST, the reverse of the expected result, so a comparator
    // that returns 0 for every pair — what aiming byTimestamp at a non-date
    // field produces — cannot pass by leaving the input order untouched.
    //
    // Each column carries its own comparator, so both headers are clicked.
    stubBindings([
      binding({
        id: 'b-jan',
        userUUID: 'newer-user',
        grantedAt: '2027-01-05T10:00:00Z',
        expiresAt: '2027-01-06T10:00:00Z'
      }),
      binding({
        id: 'b-sep',
        userUUID: 'older-user',
        grantedAt: '2026-09-05T10:00:00Z',
        expiresAt: '2026-09-06T10:00:00Z'
      })
    ]);
    renderWithProviders(<BindingsTable tenantId={TENANT} />);
    const user = userEvent.setup();

    await screen.findByText(/older-user/);

    for (const header of [/^granted$/i, /^expires$/i]) {
      await user.click(screen.getByText(header));
      await waitFor(() => {
        const order = [...document.querySelectorAll('tbody tr')]
          .map(r => (r.textContent ?? '').match(/(older|newer)-user/))
          .filter(Boolean)
          .map(m => m![0]);
        expect(order).toEqual(['older-user', 'newer-user']);
      });
    }
  });

  it('keeps a never-expiring binding out of the comparator', async () => {
    // The accessor returns `undefined` — not the "never" label the cell
    // prints — because TanStack short-circuits undefined via `sortUndefined`
    // BEFORE the comparator runs. Formatting that cell's value instead would
    // hand byTimestamp an unparseable string, which collapses to epoch and
    // would sort never-expiring rows in among the dated ones.
    //
    // Asserted direction-agnostically on purpose: TanStack derives the
    // first-click direction from the FIRST row's value type
    // (`getAutoSortDir` — string means ascending, anything else descending),
    // and here that value is the undefined one, so the first click lands
    // descending. Pinning a literal order would pin that accident.
    stubBindings([
      binding({
        id: 'b-never',
        userUUID: 'never-user',
        grantedAt: '2026-09-05T10:00:00Z'
        // no expiresAt
      }),
      binding({
        id: 'b-sep',
        userUUID: 'sep-user',
        grantedAt: '2026-09-06T10:00:00Z',
        expiresAt: '2026-09-30T10:00:00Z'
      }),
      binding({
        id: 'b-jan',
        userUUID: 'jan-user',
        grantedAt: '2026-09-07T10:00:00Z',
        expiresAt: '2027-01-05T10:00:00Z'
      })
    ]);
    renderWithProviders(<BindingsTable tenantId={TENANT} />);
    const user = userEvent.setup();

    const neverRow = (await screen.findByText(/never-user/)).closest('tr')!;
    // Column 3 is "Expires" — the row with no expiry prints the label there.
    expect(neverRow.querySelectorAll('td')[3].textContent).toMatch(/never/i);

    const order = () =>
      [...document.querySelectorAll('tbody tr')]
        .map(r => (r.textContent ?? '').match(/(never|sep|jan)-user/))
        .filter(Boolean)
        .map(m => m![0]);

    const header = screen.getByText(/^expires$/i);
    await user.click(header);
    await waitFor(() => expect(order()).toHaveLength(3));
    const first = order();

    await user.click(header);
    await waitFor(() => expect(order()).not.toEqual(first));
    const second = order();

    // Flipping direction reverses the whole column.
    expect(second).toEqual([...first].reverse());

    for (const seen of [first, second]) {
      // The undefined row sits at an end, never between the dated ones.
      expect(seen.indexOf('never-user')).not.toBe(1);
      // And the two real dates stay chronological relative to each other:
      // Sep 2026 before Jan 2027 ascending, after it descending.
      const sep = seen.indexOf('sep-user');
      const jan = seen.indexOf('jan-user');
      const neverFirst = seen[0] === 'never-user';
      expect(sep < jan).toBe(neverFirst ? false : true);
    }
  });
});
