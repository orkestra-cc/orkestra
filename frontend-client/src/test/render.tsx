import type { PropsWithChildren, ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, type InitialEntry } from "react-router";
import {
  render,
  waitFor,
  type RenderOptions,
  type RenderResult,
} from "@testing-library/react";

import { AuthProvider } from "@/auth/AuthProvider";

// One QueryClient per test. retry:false — the app's retry:1 (main.tsx)
// would put a backoff in front of every error-path assertion.
export const makeQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false, staleTime: 0 },
    },
  });

interface ProvidersRenderOptions extends Omit<RenderOptions, "queries"> {
  // Initial URL(s) for the in-memory router. Defaults to "/". Accepts
  // InitialEntry so a test can seed search/hash (the OAuth callback page).
  routerEntries?: InitialEntry[];
  queryClient?: QueryClient;
}

export interface RenderWithProvidersResult extends RenderResult {
  queryClient: QueryClient;
}

// renderWithProviders — the single entry point for component tests. Same
// provider stack as main.tsx (QueryClientProvider → AuthProvider → Router)
// with a MemoryRouter in place of BrowserRouter. Pair with the MSW server
// in src/test/server.ts: stub HTTP, never the query hooks.
export function renderWithProviders(
  ui: ReactElement,
  {
    routerEntries = ["/"],
    queryClient = makeQueryClient(),
    ...renderOptions
  }: ProvidersRenderOptions = {},
): RenderWithProvidersResult {
  const Wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <MemoryRouter initialEntries={routerEntries}>{children}</MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
  // Annotate explicitly: without a contextual type here, TS's overload
  // resolution for RTL's generic render() can't pin its query-map type
  // parameter from this call site and infers it away to nothing, silently
  // dropping every bound query method from the return type. Mirrors the
  // same fix in frontend-admin/src/test/render.tsx.
  const renderResult: RenderResult = render(ui, {
    wrapper: Wrapper,
    ...renderOptions,
  });
  return { queryClient, ...renderResult };
}

// waitForQuerySettled — resolves once the query under `queryKey` is no
// longer pending (success or error). Policy-gated UI can be byte-identical
// before and after its query lands when the answer matches the fail-open
// default; an absence assertion made against the first paint then passes
// vacuously. Anchor on the cache entry when no DOM anchor exists; prefer a
// DOM anchor whenever one does.
export const waitForQuerySettled = (
  queryClient: QueryClient,
  queryKey: readonly unknown[],
) =>
  waitFor(() => {
    const state = queryClient.getQueryState(queryKey);
    if (!state || state.status === "pending") {
      throw new Error(`${JSON.stringify(queryKey)} has not settled yet`);
    }
  });
