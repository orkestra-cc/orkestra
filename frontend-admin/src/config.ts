export type ThemeVariant = 'light' | 'dark' | 'auto';
export type NavbarPosition = 'vertical' | 'horizontal' | 'combo' | 'top' | 'double-top';
export type NavbarStyle = 'transparent' | 'card' | 'vibrant';

export interface AppSettings {
  isFluid: boolean;
  isRTL: boolean;
  isDark: boolean;
  theme: ThemeVariant;
  navbarPosition: NavbarPosition;
  showBurgerMenu: boolean;
  currency: string;
  isNavbarVerticalCollapsed: boolean;
  navbarStyle: NavbarStyle;
}

export const version: string = '0.0.1';
export const navbarBreakPoint: string = 'xl'; // Vertical navbar breakpoint
export const topNavbarBreakpoint: string = 'lg';
export const themeVariants: readonly ThemeVariant[] = ['light', 'dark', 'auto'] as const;

export const settings: AppSettings = {
  isFluid: true,
  isRTL: false,
  isDark: true,
  theme: 'dark',
  navbarPosition: 'vertical',
  showBurgerMenu: false, // controls showing vertical nav on mobile
  currency: '$',
  isNavbarVerticalCollapsed: false, // toggle vertical navbar collapse
  navbarStyle: 'transparent'
};

export default { version, navbarBreakPoint, topNavbarBreakpoint, settings };
