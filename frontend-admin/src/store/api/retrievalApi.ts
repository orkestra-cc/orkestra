import { baseApi } from './baseApi';

// retrievalApi wraps the core retrieval module's admin surface (ADR-0023):
// the installation-wide corpus report, diagnostics and drop (system
// permissions), the tenant's indexing progress and the audited search log.
// Diagnostics and drop are step-up-gated on the backend; baseApi's 401
// interceptor drives the StepUpModal and replays the request. Shapes mirror
// backend/internal/core/retrieval/models/admin.go (hand-written: there is
// no generated client).

export type RetrievalComponentStatus =
  'ok' | 'not_configured' | 'unavailable' | 'mismatch' | 'wrong_kind';

export interface RetrievalEngineStatus {
  status: 'ok' | 'not_configured' | 'unavailable';
}

export interface RetrievalInferenceStatus {
  endpoint: string;
  kind: 'embed' | 'rerank';
  status: RetrievalComponentStatus;
  profile?: string;
  boundary?: 'local' | 'hosted';
}

export interface RetrievalBacklog {
  pending: number;
  oldestAt?: string;
}

export type RetrievalLocality = 'local_only' | 'hosted_allowed';

export type RetrievalCorpusState =
  'uninitialized' | 'active' | 'profile_changed' | 'error';

export type RetrievalCorpusReason =
  'locality' | 'endpoint_unknown' | 'endpoint_kind' | 'unknown_state';

export interface RetrievalCorpusStatus {
  name: string;
  owner?: string;
  ownerRunning: boolean;
  registered: boolean;
  locality?: RetrievalLocality;
  embedEndpoint?: string;
  rerankEnabled: boolean;
  state: RetrievalCorpusState;
  reason?: RetrievalCorpusReason;
  // 0 = none; an error corpus with one still serves it.
  activeGeneration: number;
  collection?: string;
  ingestionProfile?: string;
  embeddingProfile?: string;
  outbox: RetrievalBacklog;
  tenantsDone: number;
  tenantsBuilding: number;
}

export interface RetrievalErasureCounts {
  pending: number;
  fencing: number;
}

export interface RetrievalCorporaReport {
  engine: RetrievalEngineStatus;
  inference: RetrievalInferenceStatus[];
  corpora: RetrievalCorpusStatus[];
  erasures: RetrievalErasureCounts;
}

export interface RetrievalTenantCorpus {
  corpus: string;
  owner: string;
  ownerRunning: boolean;
  // The corpus state as the operator report has it, without the reason.
  state: RetrievalCorpusState;
  initialBuild: 'pending' | 'building' | 'done';
  points?: number;
  outbox: RetrievalBacklog;
}

export interface RetrievalTenantIndexingReport {
  tenantId: string;
  engine: RetrievalEngineStatus;
  corpora: RetrievalTenantCorpus[];
  erasuresPending: number;
}

export type RetrievalOp = 'eq' | 'in' | 'lt' | 'lte' | 'gt' | 'gte' | 'exists';

export interface RetrievalFilter {
  and?: RetrievalFilter[];
  or?: RetrievalFilter[];
  not?: RetrievalFilter;
  field?: string;
  op?: RetrievalOp;
  value?: unknown;
  values?: unknown[];
}

export type RetrievalMode = 'hybrid' | 'dense' | 'keyword';

export interface RetrievalDiagnosticsRequest {
  query: string;
  filter?: RetrievalFilter;
  mode?: RetrievalMode;
  limit?: number;
}

export interface RetrievalDiagnosticHit {
  sourceId: string;
  chunkId: string;
  rank: number;
  score: number;
  fused: number;
  dense?: number;
  keyword?: number;
  rerank?: number;
  snippet: string;
}

export interface RetrievalDiagnosticsReport {
  corpus: string;
  collection: string;
  profile: string;
  principals: string[];
  degraded: boolean;
  hits: RetrievalDiagnosticHit[];
}

export type RetrievalSearchOutcome = 'ok' | 'degraded' | 'error';

export interface RetrievalSearchLogEntry {
  id: string;
  createdAt: string;
  userUuid: string;
  corpus: string;
  query: string;
  // The decoded canonical filter JSON, numbers kept exact; shape unknown.
  filter?: unknown;
  // The stored canonical filter JSON, verbatim; present exactly when
  // `filter` is. Display this text and never JSON.parse/stringify it:
  // integers beyond 2^53 would be rounded.
  filterText?: string;
  mode: string;
  principals: string[];
  returnedSourceIds: string[];
  subjects: string[];
  hitCount: number;
  degraded: boolean;
  outcome: RetrievalSearchOutcome;
  errorKind?: string;
  durationMs: number;
}

export interface RetrievalSearchLogPage {
  items: RetrievalSearchLogEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface RetrievalSearchLogQuery {
  userUuid?: string;
  since?: string;
  until?: string;
  limit: number;
  offset: number;
}

const BASE = '/v1/admin/retrieval';

export const retrievalApi = baseApi.injectEndpoints({
  endpoints: build => ({
    getRetrievalCorpora: build.query<RetrievalCorporaReport, void>({
      query: () => `${BASE}/corpora`,
      providesTags: ['RetrievalCorpora']
    }),
    runRetrievalDiagnostics: build.mutation<
      RetrievalDiagnosticsReport,
      { corpus: string } & RetrievalDiagnosticsRequest
    >({
      query: ({ corpus, ...body }) => ({
        url: `${BASE}/corpora/${encodeURIComponent(corpus)}/diagnostics`,
        method: 'POST',
        body
      })
    }),
    dropRetrievalCorpus: build.mutation<void, { corpus: string }>({
      query: ({ corpus }) => ({
        url: `${BASE}/corpora/${encodeURIComponent(corpus)}`,
        method: 'DELETE'
      }),
      invalidatesTags: ['RetrievalCorpora']
    }),
    getRetrievalTenantIndexing: build.query<
      RetrievalTenantIndexingReport,
      void
    >({
      query: () => `${BASE}/tenant/indexing`,
      providesTags: ['RetrievalTenantIndexing']
    }),
    listRetrievalSearchLog: build.query<
      RetrievalSearchLogPage,
      RetrievalSearchLogQuery
    >({
      query: ({ userUuid, since, until, limit, offset }) => {
        const params: Record<string, string | number> = {};
        if (userUuid) params.userUuid = userUuid;
        if (since) params.since = since;
        if (until) params.until = until;
        params.limit = limit;
        params.offset = offset;
        return { url: `${BASE}/search-log`, params };
      },
      providesTags: ['RetrievalSearchLog'],
      // Every view is a fresh, audited read.
      keepUnusedDataFor: 0
    })
  })
});

export const {
  useGetRetrievalCorporaQuery,
  useRunRetrievalDiagnosticsMutation,
  useDropRetrievalCorpusMutation,
  useGetRetrievalTenantIndexingQuery,
  useListRetrievalSearchLogQuery
} = retrievalApi;
