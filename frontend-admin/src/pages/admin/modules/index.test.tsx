import { describe, it, expect, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import ModuleManagementPage from './index';

const present = {
  moduleName: 'user',
  displayName: 'User',
  description: '',
  category: 'core',
  enabled: true,
  status: 'running',
  needsRestart: false,
  configValues: {},
  secretStatus: {},
  configSchema: [],
  dependsOn: [],
  providedServices: [],
  requiredServices: [],
  optionalServices: [],
  activeEnvironment: 'production',
  availableEnvironments: ['production'],
  createdAt: '',
  updatedAt: ''
};
const missing = {
  ...present,
  moduleName: 'auth',
  displayName: 'Auth',
  status: 'missing',
  missing: true
};

describe('ModuleManagementPage KPI strip', () => {
  beforeEach(() => {
    server.use(
      http.get(url('/v1/admin/modules'), () =>
        HttpResponse.json({ modules: [present, missing] })
      ),
      http.get(url('/v1/admin/modules/health'), () =>
        HttpResponse.json({ modules: [] })
      )
    );
  });

  // `missing` is a status the API can return like any other, so the strip
  // must count it — otherwise its total silently stops matching the sum of
  // the states it lists, and the one state that needs an operator is the
  // one that goes unmentioned.
  it('counts a module whose persisted document is missing', async () => {
    renderWithProviders(<ModuleManagementPage />);
    // The page-header strip only — `1 running` also appears in each
    // ModuleTable's own footer, so it is not a unique anchor here.
    expect(await screen.findByText(/1 missing/)).toBeInTheDocument();
  });
});
