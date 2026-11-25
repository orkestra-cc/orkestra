import { describe, it, expect, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders, waitForQuerySettled } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import ModulePreferencesCards from './ModulePreferencesCards';

const crm = {
  module: 'crm',
  fields: [
    {
      key: 'pageSize',
      label: 'Page size',
      type: 'int',
      required: false,
      default: '25',
      envVar: '',
      description: ''
    },
    {
      key: 'compact',
      label: 'Compact rows',
      type: 'bool',
      required: false,
      default: 'false',
      envVar: '',
      description: ''
    }
  ],
  values: { pageSize: '25', compact: 'false' }
};

describe('ModulePreferencesCards', () => {
  it('renders nothing when no module declares preferences', async () => {
    server.use(
      http.get(url('/v1/me/preferences'), () =>
        HttpResponse.json({ modules: [] })
      )
    );
    const { container, store } = renderWithProviders(
      <ModulePreferencesCards />
    );
    await waitForQuerySettled(store, 'getMyPreferencesAll');
    expect(container).toBeEmptyDOMElement();
  });

  it('hides itself on 403 (no user.self)', async () => {
    server.use(
      http.get(url('/v1/me/preferences'), () =>
        HttpResponse.json({ code: 'authz.forbidden' }, { status: 403 })
      )
    );
    const { container, store } = renderWithProviders(
      <ModulePreferencesCards />
    );
    await waitForQuerySettled(store, 'getMyPreferencesAll');
    expect(container).toBeEmptyDOMElement();
  });

  it('renders a card per module and PUTs the whole value map on save', async () => {
    const put = vi.fn();
    server.use(
      http.get(url('/v1/me/preferences'), () =>
        HttpResponse.json({ modules: [crm] })
      ),
      http.put(url('/v1/me/preferences/crm'), async ({ request }) => {
        const body = (await request.json()) as {
          values: Record<string, string>;
        };
        put(body);
        return HttpResponse.json({
          ...crm,
          values: body.values,
          updatedAt: '2026-10-28T12:00:00Z'
        });
      })
    );
    renderWithProviders(<ModulePreferencesCards />);
    expect(
      await screen.findByTestId('module-preferences-crm')
    ).toBeInTheDocument();

    const save = screen.getByRole('button', { name: /save/i });
    expect(save).toBeDisabled(); // nothing dirty yet

    const pageSize = screen.getByLabelText(/page size/i);
    await userEvent.clear(pageSize);
    await userEvent.type(pageSize, '50');
    expect(save).toBeEnabled();
    await userEvent.click(save);

    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
    // PUT semantics: every declared field travels, not only the edited one.
    expect(put.mock.calls[0][0]).toEqual({
      values: { pageSize: '50', compact: 'false' }
    });
  });
});
