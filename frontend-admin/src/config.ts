export type ThemeVariant = 'light' | 'dark' | 'auto';
export type NavbarPosition =
  | 'vertical'
  | 'horizontal'
  | 'combo'
  | 'top'
  | 'double-top';
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

// `typeof` guards against the dev server not having been restarted
// after a vite.config.js change — touching `__APP_VERSION__` directly
// would throw ReferenceError and crash the entire SPA at module load.
export const version: string =
  typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : 'dev';
export const navbarBreakPoint: string = 'xl'; // Vertical navbar breakpoint
export const topNavbarBreakpoint: string = 'lg';
export const themeVariants: readonly ThemeVariant[] = [
  'light',
  'dark',
  'auto'
] as const;

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
