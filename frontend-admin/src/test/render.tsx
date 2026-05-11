import { type PropsWithChildren, type ReactElement } from 'react';
import { configureStore, combineReducers } from '@reduxjs/toolkit';
import { Provider } from 'react-redux';
import { MemoryRouter } from 'react-router';
import { render, type RenderOptions, type RenderResult } from '@testing-library/react';

import authReducer from 'store/slices/authSlice';
import kanbanReducer from 'store/slices/kanbanSlice';
import tenantReducer from 'store/slices/tenantSlice';
import { baseApi } from 'store/api/baseApi';

// Plain (non-persisted) reducer mirroring src/store/index.ts. Tests should
// be hermetic, so redux-persist is intentionally skipped — every render
// starts from a clean state.
const rootReducer = combineReducers({
  auth: authReducer,
  tenant: tenantReducer,
  kanban: kanbanReducer,
  [baseApi.reducerPath]: baseApi.reducer,
});

export type TestRootState = ReturnType<typeof rootReducer>;
export type TestStore = ReturnType<typeof setupStore>;

export const setupStore = (preloadedState?: Partial<TestRootState>) =>
  configureStore({
    reducer: rootReducer,
    preloadedState,
    middleware: (gdm) => gdm().concat(baseApi.middleware),
  });

interface ExtendedRenderOptions extends Omit<RenderOptions, 'queries'> {
  preloadedState?: Partial<TestRootState>;
  store?: TestStore;
  // Initial URL(s) for the in-memory router. Defaults to "/".
  routerEntries?: string[];
}

export interface RenderWithProvidersResult extends RenderResult {
  store: TestStore;
}

// renderWithProviders — single entry point for component tests. Wraps the
// UI in a fresh Redux store + MemoryRouter so RTK Query, Redux selectors,
// and <Link>/<Route> all work without per-test scaffolding. Pair with the
// MSW server in src/test/server.ts to stub HTTP calls instead of mocking
// the RTK Query hooks themselves.
export function renderWithProviders(
  ui: ReactElement,
  {
    preloadedState,
    store = setupStore(preloadedState),
    routerEntries = ['/'],
    ...renderOptions
  }: ExtendedRenderOptions = {},
): RenderWithProvidersResult {
  const Wrapper = ({ children }: PropsWithChildren) => (
    <Provider store={store}>
      <MemoryRouter initialEntries={routerEntries}>{children}</MemoryRouter>
    </Provider>
  );

  const renderResult: RenderResult = render(ui, {
    wrapper: Wrapper,
    ...renderOptions,
  });
  return { store, ...renderResult };
}
