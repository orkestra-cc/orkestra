import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { url } from 'test/handlers';
import { llmApi } from './llmApi';
import { getTenantScopedTags } from './baseApi';

describe('llmApi', () => {
  it('invalidates every llm cache on an org switch', () => {
    const tags = (getTenantScopedTags() as string[]).map(String);
    for (const tag of [
      'LLMCredential',
      'LLMModel',
      'LLMMyModels',
      'LLMUsage',
      'LLMBudget',
      'LLMAccount'
    ]) {
      expect(tags).toContain(tag);
    }
  });

  it('putLlmGrants sends access and the complete user list to the model grants route', async () => {
    let body: unknown = null;
    let method: string | null = null;
    server.use(
      http.put(url('/v1/admin/llm/models/m-1/grants'), async ({ request }) => {
        body = await request.json();
        method = request.method;
        return HttpResponse.json({
          uuid: 'm-1',
          access: 'everyone',
          grants: []
        });
      })
    );

    const saved = await setupStore()
      .dispatch(
        llmApi.endpoints.putLlmGrants.initiate({
          uuid: 'm-1',
          access: 'everyone',
          userUuids: ['u-1', 'u-2']
        })
      )
      .unwrap();

    expect(method).toBe('PUT');
    expect(body).toEqual({ access: 'everyone', userUuids: ['u-1', 'u-2'] });
    // The route answers the model with its new access and grants.
    expect(saved.access).toBe('everyone');
  });

  it('createLlmCredential posts the secret once and never reads it back', async () => {
    const sent: { body?: Record<string, unknown> } = {};
    server.use(
      http.post(url('/v1/admin/llm/credentials'), async ({ request }) => {
        sent.body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          {
            uuid: 'c-1',
            name: 'OpenAI',
            provider: 'openai',
            status: 'active',
            hasSecret: true,
            secretLast4: 'cdef',
            createdAt: '',
            updatedAt: '',
            createdBy: 'u'
          },
          { status: 201 }
        );
      })
    );

    const created = await setupStore()
      .dispatch(
        llmApi.endpoints.createLlmCredential.initiate({
          name: 'OpenAI',
          provider: 'openai',
          secret: 'sk-live-abcdef'
        })
      )
      .unwrap();

    expect(sent.body?.secret).toBe('sk-live-abcdef');
    expect('secret' in created).toBe(false);
    expect(created.secretLast4).toBe('cdef');
  });

  it('patchLlmModel sends only the provided fields, uuid stays in the path', async () => {
    let body: unknown = null;
    let path: string | null = null;
    server.use(
      http.patch(url('/v1/admin/llm/models/m-1'), async ({ request }) => {
        body = await request.json();
        path = new URL(request.url).pathname;
        return HttpResponse.json({ uuid: 'm-1', grants: [] });
      })
    );

    await setupStore()
      .dispatch(
        llmApi.endpoints.patchLlmModel.initiate({
          uuid: 'm-1',
          status: 'disabled'
        })
      )
      .unwrap();

    expect(path).toBe('/v1/admin/llm/models/m-1');
    expect(body).toEqual({ status: 'disabled' });
  });

  it('getLlmBudget turns the 204 of an org without a budget into null', async () => {
    server.use(
      http.get(
        url('/v1/admin/llm/budget'),
        () => new HttpResponse(null, { status: 204 })
      )
    );
    const budget = await setupStore()
      .dispatch(llmApi.endpoints.getLlmBudget.initiate('t1'))
      .unwrap();
    expect(budget).toBeNull();
  });

  it('putLlmBudget sends the whole budget with PUT', async () => {
    let body: unknown = null;
    server.use(
      http.put(url('/v1/admin/llm/budget'), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({
          ...(body as object),
          updatedBy: 'u-1',
          updatedAt: '2026-10-10T00:00:00Z'
        });
      })
    );
    await setupStore()
      .dispatch(
        llmApi.endpoints.putLlmBudget.initiate({
          monthlyTokenLimit: 1000,
          warnPct: 80,
          action: 'warn_only',
          notifyExtraUserUuids: ['u-2']
        })
      )
      .unwrap();
    expect(body).toEqual({
      monthlyTokenLimit: 1000,
      warnPct: 80,
      action: 'warn_only',
      notifyExtraUserUuids: ['u-2']
    });
  });

  it('getLlmUsage passes groupBy and the window as query params', async () => {
    let params: URLSearchParams | null = null;
    server.use(
      http.get(url('/v1/admin/llm/usage'), ({ request }) => {
        params = new URL(request.url).searchParams;
        return HttpResponse.json({ groupBy: 'user', rows: [], month: {} });
      })
    );
    await setupStore()
      .dispatch(
        llmApi.endpoints.getLlmUsage.initiate({
          from: '2026-10-01T00:00:00.000Z',
          groupBy: 'user'
        })
      )
      .unwrap();
    const p = params as unknown as URLSearchParams;
    expect(p.get('groupBy')).toBe('user');
    expect(p.get('from')).toBe('2026-10-01T00:00:00.000Z');
    expect(p.has('to')).toBe(false);
  });

  it('promptTestLlmModel posts only the prompt; discovery and test are bodiless POSTs', async () => {
    const seen: { method: string; path: string; body: string }[] = [];
    const record = async ({ request }: { request: Request }) => {
      seen.push({
        method: request.method,
        path: new URL(request.url).pathname,
        body: await request.text()
      });
    };
    server.use(
      http.post(url('/v1/admin/llm/models/m-1/prompt'), async args => {
        await record(args);
        return HttpResponse.json({
          text: 'hi',
          usage: { inputTokens: 1, outputTokens: 1 },
          finishReason: 'stop',
          fallback: false
        });
      }),
      http.post(url('/v1/admin/llm/credentials/c-1/models'), async args => {
        await record(args);
        return HttpResponse.json({ items: [{ id: 'gpt-x' }] });
      }),
      http.post(url('/v1/admin/llm/credentials/c-1/test'), async args => {
        await record(args);
        return HttpResponse.json({ outcome: 'ok', testedAt: '' });
      })
    );
    const store = setupStore();
    await store
      .dispatch(
        llmApi.endpoints.promptTestLlmModel.initiate({
          uuid: 'm-1',
          prompt: 'Hello'
        })
      )
      .unwrap();
    const found = await store
      .dispatch(llmApi.endpoints.discoverLlmModels.initiate('c-1'))
      .unwrap();
    const tested = await store
      .dispatch(llmApi.endpoints.testLlmCredential.initiate('c-1'))
      .unwrap();
    expect(seen[0]).toEqual({
      method: 'POST',
      path: '/v1/admin/llm/models/m-1/prompt',
      body: JSON.stringify({ prompt: 'Hello' })
    });
    expect(seen.slice(1).map(s => s.body)).toEqual(['', '']);
    expect(found.items[0].id).toBe('gpt-x');
    expect(tested.outcome).toBe('ok');
  });

  it('getMyLlmUsage reads the caller own usage', async () => {
    server.use(
      http.get(url('/v1/llm/me/usage'), () =>
        HttpResponse.json({
          period: '2026-10',
          requests: 3,
          orgInputTokens: 1,
          orgOutputTokens: 2,
          userAccountInputTokens: 3,
          userAccountOutputTokens: 4
        })
      )
    );
    const mine = await setupStore()
      .dispatch(llmApi.endpoints.getMyLlmUsage.initiate())
      .unwrap();
    expect(mine.requests).toBe(3);
  });

  it('linked-account routes: link and complete carry only their bodies, select is a bodiless PUT', async () => {
    const seen: { method: string; path: string; body: string }[] = [];
    const record = async (request: Request) => {
      seen.push({
        method: request.method,
        path: new URL(request.url).pathname,
        body: await request.text()
      });
    };
    server.use(
      http.post(url('/v1/llm/accounts/openai/link'), async ({ request }) => {
        await record(request);
        return HttpResponse.json({
          authorizeUrl:
            'https://auth.openai.com/api/accounts/authorize?state=s',
          mode: 'hosted_paste'
        });
      }),
      http.post(
        url('/v1/llm/accounts/openai/complete'),
        async ({ request }) => {
          await record(request);
          return HttpResponse.json({ uuid: 'a-1', label: 'Work' });
        }
      ),
      http.put(
        url('/v1/llm/accounts/openai/a-1/selected'),
        async ({ request }) => {
          await record(request);
          return new HttpResponse(null, { status: 204 });
        }
      ),
      http.delete(url('/v1/admin/llm/accounts/a-1'), async ({ request }) => {
        await record(request);
        return new HttpResponse(null, { status: 204 });
      })
    );
    const store = setupStore();
    const started = await store
      .dispatch(
        llmApi.endpoints.linkLlmAccount.initiate({ reauthAccountUuid: 'a-1' })
      )
      .unwrap();
    await store
      .dispatch(
        llmApi.endpoints.completeLlmAccount.initiate({
          callbackUrl: 'http://127.0.0.1:1455/cb?code=c&state=s'
        })
      )
      .unwrap();
    await store
      .dispatch(llmApi.endpoints.selectLlmAccount.initiate('a-1'))
      .unwrap();
    await store
      .dispatch(llmApi.endpoints.forceUnlinkLlmAccount.initiate('a-1'))
      .unwrap();
    expect(started.mode).toBe('hosted_paste');
    expect(seen).toEqual([
      {
        method: 'POST',
        path: '/v1/llm/accounts/openai/link',
        body: JSON.stringify({ reauthAccountUuid: 'a-1' })
      },
      {
        method: 'POST',
        path: '/v1/llm/accounts/openai/complete',
        body: JSON.stringify({
          callbackUrl: 'http://127.0.0.1:1455/cb?code=c&state=s'
        })
      },
      { method: 'PUT', path: '/v1/llm/accounts/openai/a-1/selected', body: '' },
      { method: 'DELETE', path: '/v1/admin/llm/accounts/a-1', body: '' }
    ]);
  });
});
