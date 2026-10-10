import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { url } from 'test/handlers';
import { llmApi } from './llmApi';

describe('llmApi', () => {
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
});
