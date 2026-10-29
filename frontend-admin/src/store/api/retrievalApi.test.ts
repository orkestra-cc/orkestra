import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { url } from 'test/handlers';
import { retrievalApi } from './retrievalApi';
import { getTenantScopedTags } from './baseApi';

describe('retrievalApi', () => {
  it('tenant-scoped retrieval caches are dropped on an org switch, the corpus report is not', () => {
    const tags = (getTenantScopedTags() as string[]).map(String);
    expect(tags).toContain('RetrievalTenantIndexing');
    expect(tags).toContain('RetrievalSearchLog');
    expect(tags).not.toContain('RetrievalCorpora');
  });

  it('listRetrievalSearchLog sends only the filters that are set', async () => {
    const searches: string[] = [];
    server.use(
      http.get(url('/v1/admin/retrieval/search-log'), ({ request }) => {
        searches.push(new URL(request.url).search);
        return HttpResponse.json({
          items: [],
          total: 0,
          limit: 50,
          offset: 0
        });
      })
    );
    const store = setupStore();
    await store
      .dispatch(
        retrievalApi.endpoints.listRetrievalSearchLog.initiate({
          limit: 50,
          offset: 0
        })
      )
      .unwrap();
    await store
      .dispatch(
        retrievalApi.endpoints.listRetrievalSearchLog.initiate({
          userUuid: 'u-1',
          since: '2026-10-01T00:00:00Z',
          limit: 20,
          offset: 40
        })
      )
      .unwrap();

    expect(searches).toEqual([
      '?limit=50&offset=0',
      '?userUuid=u-1&since=2026-10-01T00%3A00%3A00Z&limit=20&offset=40'
    ]);
  });

  it('dropRetrievalCorpus deletes the corpus route and refetches the report', async () => {
    let deleted: string | null = null;
    let reads = 0;
    server.use(
      http.get(url('/v1/admin/retrieval/corpora'), () => {
        reads++;
        return HttpResponse.json({
          engine: { status: 'ok' },
          inference: [],
          corpora: [],
          erasures: { pending: 0, fencing: 0 }
        });
      }),
      http.delete(url('/v1/admin/retrieval/corpora/crm'), ({ request }) => {
        deleted = request.method;
        return new HttpResponse(null, { status: 204 });
      })
    );
    const store = setupStore();
    const sub = store.dispatch(
      retrievalApi.endpoints.getRetrievalCorpora.initiate()
    );
    await sub;
    expect(reads).toBe(1);

    await store
      .dispatch(
        retrievalApi.endpoints.dropRetrievalCorpus.initiate({ corpus: 'crm' })
      )
      .unwrap();

    expect(deleted).toBe('DELETE');
    await new Promise(r => setTimeout(r, 50));
    expect(reads).toBe(2);
    sub.unsubscribe();
  });
});
