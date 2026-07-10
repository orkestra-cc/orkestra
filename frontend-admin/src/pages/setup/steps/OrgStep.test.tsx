import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, it, expect, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import OrgStep from './OrgStep';

describe('OrgStep', () => {
  it('skip advances without creating an organization', async () => {
    const user = userEvent.setup();
    const onNext = vi.fn();
    const onSkip = vi.fn();

    renderWithProviders(
      <OrgStep
        adminFullName="Salvatore Balestrino"
        onNext={onNext}
        onSkip={onSkip}
      />
    );

    // MSW is configured with onUnhandledRequest: 'error', so if the skip path
    // fired POST /v1/tenants the test would fail on an unhandled request.
    await user.click(screen.getByRole('button', { name: /skip for now/i }));

    expect(onSkip).toHaveBeenCalledTimes(1);
    expect(onNext).not.toHaveBeenCalled();
  });

  it('renders the create button', () => {
    renderWithProviders(
      <OrgStep
        adminFullName="Salvatore Balestrino"
        onNext={vi.fn()}
        onSkip={vi.fn()}
      />
    );
    expect(
      screen.getByRole('button', { name: /create organization/i })
    ).toBeInTheDocument();
  });

  it('create submits POST /v1/tenants and advances via onNext', async () => {
    const user = userEvent.setup();
    const onNext = vi.fn();
    const onSkip = vi.fn();

    // The create path hits POST /v1/tenants; createOrg.onQueryStarted then
    // refreshes the access token via GET /v1/auth/session so the new
    // membership lands in the JWT. Stub both so MSW's strict
    // onUnhandledRequest:'error' stays quiet — a 401 session is handled
    // gracefully by getSession (expected unauthenticated state), keeping the
    // test output pristine without constructing a full user payload.
    server.use(
      http.post(url('/v1/tenants'), () =>
        HttpResponse.json({
          id: 'org-new',
          name: "Salvatore's Workspace",
          slug: 'salvatore-s-workspace',
          ownerUserUUID: 'user-1',
          plan: 'enterprise',
          features: [],
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z'
        })
      ),
      http.get(
        url('/v1/auth/session'),
        () => new HttpResponse(null, { status: 401 })
      )
    );

    renderWithProviders(
      <OrgStep
        adminFullName="Salvatore Balestrino"
        onNext={onNext}
        onSkip={onSkip}
      />
    );

    // The name field is pre-filled with "{firstName}'s Workspace", so a single
    // click on the primary button exercises the create path end-to-end.
    await user.click(
      screen.getByRole('button', { name: /create organization/i })
    );

    await waitFor(() =>
      expect(onNext).toHaveBeenCalledWith("Salvatore's Workspace")
    );
    expect(onSkip).not.toHaveBeenCalled();
  });
});
