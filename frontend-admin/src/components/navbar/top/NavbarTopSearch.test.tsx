import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { NavbarPosition } from 'config';
import NavbarTop from './NavbarTop';
import VerticalLayoutNavbarTop from './VerticalLayoutNavbarTop';
import { DoubleTopLayoutNavbarTop } from './DoubleTopLayoutNavbarTop';

// The top bar's search box searched the theme's demo data, never the
// console's, so the bars no longer mount it (2026-11-13). These tests pin that
// in every layout that used to show it. The right-hand icons and the module
// menus are stubbed: they need the auth, tenant and navigation state, and
// they are not what is under test.
const config = vi.hoisted(() => ({
  navbarPosition: 'vertical' as NavbarPosition
}));

vi.mock('providers/AppProvider', () => ({
  useAppContext: () => ({
    config: {
      showBurgerMenu: false,
      navbarCollapsed: false,
      navbarPosition: config.navbarPosition
    },
    setConfig: vi.fn()
  })
}));
vi.mock('./TopNavRightSideNavItem', () => ({
  default: () => <div data-testid="right-side-items" />
}));
vi.mock('./NavbarTopDropDownMenus', () => ({
  default: () => <li data-testid="dropdown-menus" />
}));

const expectNoSearchBox = (container: HTMLElement) => {
  expect(screen.queryByRole('searchbox')).toBeNull();
  expect(container.querySelector('.search-box')).toBeNull();
  // The right-hand icons still render, so the bar is not empty.
  expect(screen.getByTestId('right-side-items')).toBeInTheDocument();
};

describe('top bar without the demo search box', () => {
  it.each<NavbarPosition>(['vertical', 'double-top', 'top', 'combo'])(
    'NavbarTop in the %s position',
    position => {
      config.navbarPosition = position;
      const { container } = renderWithProviders(<NavbarTop />);
      expectNoSearchBox(container);
    }
  );

  it('VerticalLayoutNavbarTop', () => {
    const { container } = renderWithProviders(<VerticalLayoutNavbarTop />);
    expectNoSearchBox(container);
  });

  it('DoubleTopLayoutNavbarTop', () => {
    const { container } = renderWithProviders(<DoubleTopLayoutNavbarTop />);
    expectNoSearchBox(container);
  });
});
