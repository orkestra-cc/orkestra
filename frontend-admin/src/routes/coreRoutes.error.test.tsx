import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { type ReactElement } from 'react';
import { Route, Routes } from 'react-router';
import { describe, expect, it } from 'vitest';

import { renderWithProviders } from 'test/render';
import { buildCoreRoutes } from './coreRoutes';

describe('root route error handling', () => {
  it('shows a friendly error page that returns the operator to the dashboard', async () => {
    const [rootRoute] = buildCoreRoutes([]);

    expect(rootRoute.errorElement).toBeDefined();

    renderWithProviders(
      <Routes>
        <Route path="/" element={rootRoute.errorElement as ReactElement} />
        <Route path="/user/dashboard" element={<h1>Dashboard</h1>} />
      </Routes>
    );

    expect(
      screen.getByRole('heading', { name: /whoops, something went wrong/i })
    ).toBeInTheDocument();

    await userEvent.click(
      screen.getByRole('button', { name: /back to dashboard/i })
    );

    expect(
      screen.getByRole('heading', { name: 'Dashboard' })
    ).toBeInTheDocument();
  });
});
