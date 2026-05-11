import { settings } from 'config';
import { setItemToStore } from 'helpers/utils';

export interface ConfigState {
  [key: string]: any;
}

export interface ConfigAction {
  type: 'SET_CONFIG' | 'REFRESH' | 'RESET';
  payload?: {
    key: string;
    value: any;
    setInStore?: boolean;
  };
}

export const configReducer = (state: ConfigState, action: ConfigAction): ConfigState => {
  const { type, payload } = action;
  switch (type) {
    case 'SET_CONFIG':
      if (payload?.setInStore) {
        setItemToStore(payload.key, payload.value);
      }
      return {
        ...state,
        [payload?.key || '']: payload?.value
      };
    case 'REFRESH':
      return {
        ...state
      };
    case 'RESET':
      localStorage.clear();
      document.documentElement.setAttribute('data-bs-theme', settings.theme);
      return {
        ...state,
        ...settings
      };
    default:
      return state;
  }
};
