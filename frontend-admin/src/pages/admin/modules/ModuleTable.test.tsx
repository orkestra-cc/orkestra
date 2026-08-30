import { describe, it, expect, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import ModuleTable from './ModuleTable';

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

describe('ModuleTable missing row', () => {
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

  it('flags a required module whose document is missing, and only that one', async () => {
    renderWithProviders(<ModuleTable scope="core" />);
    expect(await screen.findByTestId('module-missing-auth')).toHaveTextContent(
      /Configuration document missing/
    );
    expect(screen.queryByTestId('module-missing-user')).not.toBeInTheDocument();
    expect(screen.getByText('missing')).toBeInTheDocument();
  });
});
