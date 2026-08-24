import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
import FinishStep from './FinishStep';

describe('FinishStep', () => {
  it('names the created tenant and renders the manual-mode summary', () => {
    renderWithProviders(
      <FinishStep
        tenantName="Acme HQ"
        allowAdditional
        smtpConfigured
        onFinish={vi.fn()}
      />
    );

    expect(screen.getAllByText('Acme HQ').length).toBeGreaterThan(0);
    expect(
      screen.getByText(/additional internal tenants any time/i)
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/no additional internal tenants can ever be created/i)
    ).not.toBeInTheDocument();
  });

  it('renders the single-mode summary', () => {
    renderWithProviders(
      <FinishStep
        tenantName="Acme HQ"
        allowAdditional={false}
        smtpConfigured
        onFinish={vi.fn()}
      />
    );

    expect(
      screen.getByText(/no additional internal tenants can ever be created/i)
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/additional internal tenants any time/i)
    ).not.toBeInTheDocument();
  });

  it('shows the SMTP warning only when SMTP is not configured', () => {
    renderWithProviders(
      <FinishStep
        tenantName="Acme HQ"
        allowAdditional={false}
        smtpConfigured={false}
        onFinish={vi.fn()}
      />
    );

    expect(screen.getByText(/smtp is not configured/i)).toBeInTheDocument();
  });
});
