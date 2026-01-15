import React, { createContext, useContext, useEffect, useReducer, ReactNode } from 'react';
import { settings, AppSettings, ThemeVariant, NavbarPosition } from 'config';
import { getColor, getItemFromStore } from 'helpers/utils';
import useToggleStyle from 'hooks/ui/useToggleStyle';
import { configReducer } from 'reducers/configReducer';

// Extended config state with additional UI properties
interface AppConfigState extends AppSettings {
  disabledNavbarPosition: NavbarPosition[];
  showSettingPanel: boolean;
  navbarCollapsed: boolean;
  openAuthModal: boolean;
}

// Context value interface
interface AppContextValue {
  config: AppConfigState;
  setConfig: (key: keyof AppConfigState, value: any) => void;
  configDispatch: React.Dispatch<ConfigAction>;
  changeTheme: (theme: ThemeVariant) => void;
  getThemeColor: (name: string) => string;
}

// Action types for reducer
interface SetConfigAction {
  type: 'SET_CONFIG';
  payload: {
    key: keyof AppConfigState;
    value: any;
    setInStore: boolean;
  };
}

interface RefreshAction {
  type: 'REFRESH';
}

interface ResetAction {
  type: 'RESET';
}

type ConfigAction = SetConfigAction | RefreshAction | ResetAction;

interface AppProviderProps {
  children: ReactNode;
}

export const AppContext = createContext<AppContextValue | undefined>(undefined);

const AppProvider: React.FC<AppProviderProps> = ({ children }) => {
  const configState = {
    isFluid: getItemFromStore('isFluid', settings.isFluid),
    isRTL: getItemFromStore('isRTL', settings.isRTL),
    isDark: getItemFromStore('isDark', settings.isDark),
    theme: getItemFromStore('theme', settings.theme),
    navbarPosition: getItemFromStore('navbarPosition', settings.navbarPosition),
    disabledNavbarPosition: [],
    isNavbarVerticalCollapsed: getItemFromStore(
      'isNavbarVerticalCollapsed',
      settings.isNavbarVerticalCollapsed
    ),
    navbarStyle: getItemFromStore('navbarStyle', settings.navbarStyle),
    currency: settings.currency,
    showBurgerMenu: settings.showBurgerMenu,
    showSettingPanel: false,
    navbarCollapsed: false,
    openAuthModal: false
  };

  const [config, configDispatch] = useReducer(configReducer, configState);

  const setConfig = (key: keyof AppConfigState, value: any) => {
    configDispatch({
      type: 'SET_CONFIG',
      payload: {
        key,
        value,
        setInStore: [
          'isFluid',
          'isRTL',
          'isDark',
          'theme',
          'navbarPosition',
          'isNavbarVerticalCollapsed',
          'navbarStyle'
        ].includes(key)
      }
    });
  };
  const { isLoaded } = useToggleStyle(config.isRTL, config.isDark);

  useEffect(() => {
    const isDark =
      config.theme === 'auto'
        ? window.matchMedia('(prefers-color-scheme: dark)').matches
        : config.theme === 'dark';

    setConfig('isDark', isDark);
  }, [config.theme]);

  const changeTheme = (theme: ThemeVariant) => {
    const isDark =
      theme === 'auto'
        ? window.matchMedia('(prefers-color-scheme: dark)').matches
        : theme === 'dark';

    document.documentElement.setAttribute(
      'data-bs-theme',
      isDark ? 'dark' : 'light'
    );

    setConfig('theme', theme);
    setConfig('isDark', isDark);
  };

  const getThemeColor = (name: string): string => getColor(name);

  if (!isLoaded) {
    return (
      <div
        style={{
          position: 'fixed',
          top: 0,
          right: 0,
          bottom: 0,
          left: 0,
          backgroundColor: config.isDark
            ? getThemeColor('dark')
            : getThemeColor('light')
        }}
      />
    );
  }

  const value: AppContextValue = {
    config: config as AppConfigState,
    setConfig,
    configDispatch,
    changeTheme,
    getThemeColor
  };

  return (
    <AppContext.Provider value={value}>
      {children}
    </AppContext.Provider>
  );
};

export const useAppContext = (): AppContextValue => {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error('useAppContext must be used within AppProvider');
  }
  return context;
};

export default AppProvider;
