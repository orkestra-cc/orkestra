import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { renderWithProviders } from 'test/render';
import ProvisioningPolicyCard from './ProvisioningPolicyCard';

const policyHandler = (body: {
  internal: string;
  external: string;
  internalCount: number;
  externalCount: number;
}) =>
  http.get('*/v1/admin/tenants/provisioning-policy', () =>
    HttpResponse.json(body)
  );

describe('ProvisioningPolicyCard', () => {
  it("links at the tenant module's own provisioning section for the internal tier", async () => {
    server.use(
      policyHandler({
        internal: 'single',
        external: 'open',
        internalCount: 1,
        externalCount: 0
      })
    );
    renderWithProviders(<ProvisioningPolicyCard tier="internal" />);

    expect(await screen.findByText('Single tenant')).toBeInTheDocument();
    const link = screen.getByRole('link', { name: /manage policy/i });
    expect(link).toHaveAttribute(
      'href',
      '/admin/modules/tenant?section=provisioning.internal'
    );
  });

  it("links at the tenant module's own provisioning section for the external tier, and still renders the open mode", async () => {
    server.use(
      policyHandler({
        internal: 'manual',
        external: 'open',
        internalCount: 1,
        externalCount: 4
      })
    );
    renderWithProviders(<ProvisioningPolicyCard tier="external" />);

    expect(await screen.findByText('Open')).toBeInTheDocument();
    const link = screen.getByRole('link', { name: /manage policy/i });
    expect(link).toHaveAttribute(
      'href',
      '/admin/modules/tenant?section=provisioning.external'
    );
  });

  // Internal is fail-closed on the backend (manual|single, never open) —
  // the loading-state placeholder must not borrow 'open' styling either,
  // since 'open' can never legitimately describe this tier once loaded.
  it('never falls back to the open-mode styling while the internal policy is loading', () => {
    server.use(
      http.get(
        '*/v1/admin/tenants/provisioning-policy',
        () => new Promise(() => {}) // never resolves during this test
      )
    );
    const { container } = renderWithProviders(
      <ProvisioningPolicyCard tier="internal" />
    );

    const card = container.querySelector('.card');
    expect(card).not.toBeNull();
    expect(card?.className).not.toMatch(/border-success/);
  });
});
