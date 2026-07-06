import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { renderWithProviders } from 'test/render';
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
});
