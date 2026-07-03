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

// Surfaced in the footer. Vite auto-exposes VITE_*-prefixed env vars on
// `import.meta.env`, so this stays a plain runtime lookup with no special
// build-time substitution needed. vite.config.js::resolveAppVersion()
// computes the value and assigns it to process.env.VITE_APP_VERSION
// before Vite's loadEnv runs.
export const version: string = import.meta.env.VITE_APP_VERSION || 'dev';
export const navbarBreakPoint: string = 'md'; // Vertical navbar breakpoint
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
