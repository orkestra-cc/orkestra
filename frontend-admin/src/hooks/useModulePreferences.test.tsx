import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { renderHook, waitFor } from '@testing-library/react';
import { Provider } from 'react-redux';
import { setupStore } from 'test/render';
import { server } from 'test/server';
import { url } from 'test/handlers';
import { useModulePreferences } from './useModulePreferences';

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <Provider store={setupStore()}>{children}</Provider>
);

describe('useModulePreferences', () => {
  it('loads fields and values and saves the whole map', async () => {
    let stored = { pageSize: '25' };
    server.use(
      http.get(url('/v1/me/preferences/crm'), () =>
        HttpResponse.json({
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
            }
          ],
          values: stored
        })
      ),
      http.put(url('/v1/me/preferences/crm'), async ({ request }) => {
        const body = (await request.json()) as {
          values: Record<string, string>;
        };
        stored = body.values as typeof stored;
        return HttpResponse.json({
          module: 'crm',
          fields: [],
          values: stored,
          updatedAt: '2026-10-28T12:00:00Z'
        });
      })
    );
    const { result } = renderHook(() => useModulePreferences('crm'), {
      wrapper
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.fields).toHaveLength(1);
    expect(result.current.values.pageSize).toBe('25');
    expect(result.current.updatedAt).toBeUndefined();

    const saved = await result.current.save({ pageSize: '50' });
    expect(saved.values.pageSize).toBe('50');
    await waitFor(() => expect(result.current.values.pageSize).toBe('50'));
  });

  it('surfaces a 404 for a module without preferences', async () => {
    server.use(
      http.get(url('/v1/me/preferences/nope'), () =>
        HttpResponse.json(
          { code: 'user.preferences.module_unknown' },
          { status: 404 }
        )
      )
    );
    const { result } = renderHook(() => useModulePreferences('nope'), {
      wrapper
    });
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.fields).toEqual([]);
  });
});
