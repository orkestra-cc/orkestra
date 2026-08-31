import { type PropsWithChildren, type ReactElement } from 'react';
import { configureStore, combineReducers } from '@reduxjs/toolkit';
import { Provider } from 'react-redux';
import { MemoryRouter, type InitialEntry } from 'react-router';
import {
  render,
  waitFor,
  type RenderOptions,
  type RenderResult
} from '@testing-library/react';
import { QueryStatus } from '@reduxjs/toolkit/query';

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
  [baseApi.reducerPath]: baseApi.reducer
});

export type TestRootState = ReturnType<typeof rootReducer>;
export type TestStore = ReturnType<typeof setupStore>;

export const setupStore = (preloadedState?: Partial<TestRootState>) =>
  configureStore({
    reducer: rootReducer,
    preloadedState,
    middleware: gdm => gdm().concat(baseApi.middleware),
    // Batch store notifications on a microtask instead of RTK's default
    // animation frame. The raf queue calls the unqualified global
    // `cancelAnimationFrame` from inside the frame callback, and happy-dom
    // schedules that callback on a setImmediate — so one queued just before
    // a test file ends fires after vitest has deleted the window globals,
    // throwing ReferenceError outside any test and failing the whole run.
    // A microtask always flushes in the tick that scheduled it, so it can
    // never outlive its environment. See src/test/setupStore.test.ts.
    enhancers: gde => gde({ autoBatch: { type: 'tick' } })
  });

interface ExtendedRenderOptions extends Omit<RenderOptions, 'queries'> {
  preloadedState?: Partial<TestRootState>;
  store?: TestStore;
  // Initial URL(s) for the in-memory router. Defaults to "/". Accepts
  // InitialEntry so tests can seed location.state (e.g. ProtectedRoute's
  // `from`), not just a bare path string.
  routerEntries?: InitialEntry[];
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
  }: ExtendedRenderOptions = {}
): RenderWithProvidersResult {
  const Wrapper = ({ children }: PropsWithChildren) => (
    <Provider store={store}>
      <MemoryRouter initialEntries={routerEntries}>{children}</MemoryRouter>
    </Provider>
  );

  const renderResult: RenderResult = render(ui, {
    wrapper: Wrapper,
    ...renderOptions
  });
  return { store, ...renderResult };
}

// waitForQuerySettled — resolves once the named RTK Query endpoint has a
// SETTLED cache entry (fulfilled or rejected). Policy-gated UI is often
// byte-identical before and after its query lands, because the fail-open
// default renders the same tree the enabled policy does; a DOM anchor then
// lets a following absence assertion pass vacuously against the first
// paint. Anchoring on the cache entry is honest: that state change is what
// drives the re-render, so once it is settled the gated tree has been
// evaluated with the real answer. Prefer a DOM anchor whenever one exists.
export const waitForQuerySettled = (store: TestStore, endpointName: string) =>
  waitFor(() => {
    const settled = Object.values(
      store.getState()[baseApi.reducerPath].queries
    ).some(
      entry =>
        entry?.endpointName === endpointName &&
        (entry.status === QueryStatus.fulfilled ||
          entry.status === QueryStatus.rejected)
    );
    if (!settled) throw new Error(`${endpointName} has not settled yet`);
  });
