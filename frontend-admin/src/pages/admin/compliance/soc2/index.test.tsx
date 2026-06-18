import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import SOC2EvidencePage from './index';

const evidence = {
  generatedAt: '2026-06-18T10:00:00Z',
  summary: { privileged_users: 3, failed_logins_24h: 12 },
  controls: {
    'CC6.1_logical_access': { total: 3, roles: ['administrator'] }
  }
};

describe('SOC2EvidencePage', () => {
  it('renders summary KPIs and per-control breakdown', async () => {
    server.use(
      http.get(url('/v1/admin/compliance/soc2/evidence'), () =>
        HttpResponse.json(evidence)
      )
    );
    renderWithProviders(<SOC2EvidencePage />);

    expect(await screen.findByText('Privileged Users')).toBeInTheDocument();
    expect(screen.getByText('Failed Logins (24h)')).toBeInTheDocument();
    // Control key humanized into a CC-coded section title.
    expect(screen.getByText(/CC6\.1 · Logical Access/)).toBeInTheDocument();
  });

  it('shows the disabled hint when the endpoint 404s', async () => {
    server.use(
      http.get(
        url('/v1/admin/compliance/soc2/evidence'),
        () => new HttpResponse(null, { status: 404 })
      )
    );
    renderWithProviders(<SOC2EvidencePage />);

    expect(
      await screen.findByText(/SOC2 evidence is disabled/i)
    ).toBeInTheDocument();
  });
});
