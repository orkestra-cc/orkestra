// Active-state matching for the vertical sidebar. Regression guard for the
// "/forms parent leaf stays highlighted on every /forms/* sibling route" bug:
// React Router NavLink prefix-matches, so a leaf whose `to` is a prefix of a
// sibling's `to` must be suppressed when the more specific sibling matches
// ("most specific wins"), while still highlighting the list leaf on its own
// detail routes.

import { describe, it, expect, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
import NavbarVerticalMenu from './NavbarVerticalMenu';

vi.mock('providers/AppProvider', () => ({
  useAppContext: () => ({
    config: { showBurgerMenu: false },
    setConfig: vi.fn()
  })
}));

// Mirrors the Forms submenu: the list leaf (/forms) is a prefix of its
// siblings (/forms/reports, /forms/fields).
const formsRoutes = [
  { name: 'Report', to: '/forms/reports' },
  { name: 'Gestione Forms', to: '/forms' },
  { name: 'Catalogo Campi', to: '/forms/fields' }
];

const activeHrefs = () =>
  [...document.querySelectorAll('a.nav-link.active')].map(a =>
    a.getAttribute('href')
  );

describe('NavbarVerticalMenu active state', () => {
  it('marks only the most specific sibling active on a child route', () => {
    renderWithProviders(<NavbarVerticalMenu routes={formsRoutes} />, {
      routerEntries: ['/forms/reports']
    });
    // Bug: /forms stays active too. It must not.
    expect(activeHrefs()).toEqual(['/forms/reports']);
  });

  it('marks the /forms leaf active on /forms itself', () => {
    renderWithProviders(<NavbarVerticalMenu routes={formsRoutes} />, {
      routerEntries: ['/forms']
    });
    expect(activeHrefs()).toEqual(['/forms']);
  });

  it('keeps the list leaf active on a detail route with no matching sibling', () => {
    renderWithProviders(<NavbarVerticalMenu routes={formsRoutes} />, {
      routerEntries: ['/forms/8f3a-detail']
    });
    expect(activeHrefs()).toEqual(['/forms']);
  });
});
