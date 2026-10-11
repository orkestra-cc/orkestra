import { baseApi } from './baseApi';

// llmApi wraps the core llm module (ADR-0022): credentials (secrets are
// write-only — the API returns only hasSecret/secretLast4), configured
// models with their per-user grants, and the caller's own usable models.
// Credential create/patch/rotate/delete and grant replacement are
// step-up-gated on the backend; baseApi's 401 interceptor drives the
// StepUpModal and replays the request, so nothing here handles it.
// The budget PUT is step-up-gated too. Shapes mirror
// backend/internal/core/llm/models (dto.go, credential.go, model.go,
// grant.go, ops.go, usage.go, budget.go).

export type LlmProvider =
  'openai' | 'anthropic' | 'gemini' | 'ollama' | 'openai_compatible' | 'mock';

export const LLM_PROVIDERS: LlmProvider[] = [
  'openai',
  'anthropic',
  'gemini',
  'ollama',
  'openai_compatible',
  'mock'
];

// Providers whose endpoint the operator may not change (backend
// models.FixedBaseURL).
export const LLM_FIXED_BASE_URL: Partial<Record<LlmProvider, string>> = {
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  gemini: 'https://generativelanguage.googleapis.com'
};

export type LlmStatus = 'active' | 'disabled';

export interface LlmCredential {
  uuid: string;
  name: string;
  provider: LlmProvider;
  baseUrl?: string;
  status: LlmStatus;
  hasSecret: boolean;
  secretLast4?: string;
  lastTestedAt?: string;
  lastTestStatus?: 'ok' | 'error';
  // An llm.* error code, never provider text.
  lastTestError?: string;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface LlmCapabilities {
  chat: boolean;
  streaming: boolean;
  tools: boolean;
  structuredOutput: boolean;
  embeddings: boolean;
  dimensions?: number;
}

export interface LlmCredentialRef {
  kind: 'org' | 'user_account';
  credentialUuid?: string;
}

export interface LlmModelDefaults {
  temperature?: number;
  maxOutputTokens?: number;
  effort?: '' | 'low' | 'medium' | 'high';
}

export interface LlmPurpose {
  purpose: string;
  // Lower wins.
  priority: number;
}

export interface LlmGrant {
  uuid: string;
  modelUuid: string;
  userUuid: string;
  grantedBy: string;
  createdAt: string;
}

export type LlmAccess = 'granted' | 'everyone';

export interface LlmModel {
  uuid: string;
  name: string;
  provider: LlmProvider;
  modelId: string;
  capabilities: LlmCapabilities;
  credentialRef: LlmCredentialRef;
  defaults: LlmModelDefaults;
  budgetReserveOutputTokens?: number;
  purposes: LlmPurpose[];
  access: LlmAccess;
  status: LlmStatus;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
  grants: LlmGrant[];
}

// Create body: the editable fields of a model. `access` is not one of
// them: a new model is created closed (access granted, no grants) and only
// the grants route (llm.grants.admin + step-up) decides who may use it.
export type LlmModelBody = Omit<
  LlmModel,
  | 'uuid'
  | 'status'
  | 'access'
  | 'createdBy'
  | 'createdAt'
  | 'updatedAt'
  | 'grants'
>;

// Patch body: partial update. An absent field keeps the stored value;
// capabilities, credentialRef, defaults and purposes replace it whole when
// present. The backend validates the merged model with the create rules.
export type LlmModelPatch = Partial<LlmModelBody> & { status?: LlmStatus };

export interface LlmModelInfo {
  uuid: string;
  name: string;
  provider: LlmProvider;
  modelId: string;
  capabilities: LlmCapabilities;
  credentialKind: 'org' | 'user_account';
  purposes: string[];
}

// POST …/credentials/{uuid}/test. A provider failure is a 200 with outcome
// error and an llm.* code (never provider text); the outcome is also
// recorded on the credential (lastTestStatus/lastTestedAt/lastTestError).
export interface LlmCredentialTestResult {
  outcome: 'ok' | 'error';
  code?: string;
  testedAt: string;
}

export interface LlmDiscoveredModel {
  id: string;
  displayName?: string;
}

export interface LlmUsageCounts {
  inputTokens: number;
  outputTokens: number;
}

// POST …/models/{uuid}/prompt. The answer does not name the model that
// replied; fallback says whether it was not the pinned one.
export interface LlmPromptTestResult {
  text: string;
  truncated?: boolean;
  usage: LlmUsageCounts;
  finishReason: string;
  fallback: boolean;
}

export type LlmBudgetAction = 'block' | 'warn_only';
export const LLM_BUDGET_ACTIONS: LlmBudgetAction[] = ['block', 'warn_only'];

export interface LlmBudget {
  // Input + output tokens per UTC month, both credential kinds.
  monthlyTokenLimit: number;
  // 0 disables the warning.
  warnPct: number;
  action: LlmBudgetAction;
  notifyExtraUserUuids: string[];
  updatedBy: string;
  updatedAt: string;
}

export interface LlmBudgetBody {
  monthlyTokenLimit: number;
  warnPct?: number;
  action: LlmBudgetAction;
  notifyExtraUserUuids?: string[];
}

export type LlmUsageGroupBy = 'model' | 'user' | 'caller';

export interface LlmUsageRow {
  // modelUuid, userUuid or caller, after groupBy.
  key: string;
  requests: number;
  inputTokens: number;
  outputTokens: number;
}

// The current UTC month's rollup. Consumed = the four token counters;
// reservedTokens is what calls still in flight have reserved.
export interface LlmUsageMonth {
  period: string;
  requests: number;
  orgInputTokens: number;
  orgOutputTokens: number;
  userAccountInputTokens: number;
  userAccountOutputTokens: number;
  reservedTokens: number;
  warnedAt?: string;
  exhaustedAt?: string;
  updatedAt?: string;
}

export interface LlmUsageReport {
  from: string;
  to: string;
  groupBy: LlmUsageGroupBy;
  rows: LlmUsageRow[] | null;
  month: LlmUsageMonth;
  // Absent when the org has no budget (no limit).
  budget?: LlmBudget;
}

export interface LlmUsageQuery {
  from?: string;
  to?: string;
  groupBy: LlmUsageGroupBy;
}

export interface LlmSelfUsage {
  period: string;
  requests: number;
  orgInputTokens: number;
  orgOutputTokens: number;
  userAccountInputTokens: number;
  userAccountOutputTokens: number;
}

export const llmMonthConsumed = (m?: LlmUsageMonth): number =>
  m
    ? m.orgInputTokens +
      m.orgOutputTokens +
      m.userAccountInputTokens +
      m.userAccountOutputTokens
    : 0;

export const llmApi = baseApi.injectEndpoints({
  endpoints: build => ({
    listLlmCredentials: build.query<{ items: LlmCredential[] }, void>({
      query: () => '/v1/admin/llm/credentials',
      providesTags: ['LLMCredential']
    }),
    createLlmCredential: build.mutation<
      LlmCredential,
      { name: string; provider: LlmProvider; baseUrl?: string; secret?: string }
    >({
      query: body => ({
        url: '/v1/admin/llm/credentials',
        method: 'POST',
        body
      }),
      invalidatesTags: ['LLMCredential']
    }),
    patchLlmCredential: build.mutation<
      LlmCredential,
      { uuid: string; name?: string; baseUrl?: string; status?: LlmStatus }
    >({
      query: ({ uuid, ...body }) => ({
        url: `/v1/admin/llm/credentials/${uuid}`,
        method: 'PATCH',
        body
      }),
      // A disabled credential takes its models out of the caller's own list.
      invalidatesTags: ['LLMCredential', 'LLMModel', 'LLMMyModels']
    }),
    rotateLlmCredential: build.mutation<
      LlmCredential,
      { uuid: string; secret: string }
    >({
      query: ({ uuid, secret }) => ({
        url: `/v1/admin/llm/credentials/${uuid}/rotate`,
        method: 'POST',
        body: { secret }
      }),
      invalidatesTags: ['LLMCredential']
    }),
    deleteLlmCredential: build.mutation<void, string>({
      query: uuid => ({
        url: `/v1/admin/llm/credentials/${uuid}`,
        method: 'DELETE'
      }),
      invalidatesTags: ['LLMCredential']
    }),
    listLlmModels: build.query<{ items: LlmModel[] }, void>({
      query: () => '/v1/admin/llm/models',
      providesTags: ['LLMModel']
    }),
    createLlmModel: build.mutation<LlmModel, LlmModelBody>({
      query: body => ({ url: '/v1/admin/llm/models', method: 'POST', body }),
      invalidatesTags: ['LLMModel', 'LLMMyModels']
    }),
    patchLlmModel: build.mutation<LlmModel, { uuid: string } & LlmModelPatch>({
      query: ({ uuid, ...body }) => ({
        url: `/v1/admin/llm/models/${uuid}`,
        method: 'PATCH',
        body
      }),
      invalidatesTags: ['LLMModel', 'LLMMyModels']
    }),
    deleteLlmModel: build.mutation<void, string>({
      query: uuid => ({
        url: `/v1/admin/llm/models/${uuid}`,
        method: 'DELETE'
      }),
      invalidatesTags: ['LLMModel', 'LLMMyModels', 'LLMCredential']
    }),
    // Decides who may use the model: sets access and replaces its grants
    // with exactly userUuids, together. With access 'everyone' the grants
    // are kept but dormant. Answers the model with its new access and grants.
    putLlmGrants: build.mutation<
      LlmModel,
      { uuid: string; access: LlmAccess; userUuids: string[] }
    >({
      query: ({ uuid, access, userUuids }) => ({
        url: `/v1/admin/llm/models/${uuid}/grants`,
        method: 'PUT',
        body: { access, userUuids }
      }),
      invalidatesTags: ['LLMModel', 'LLMMyModels']
    }),
    listMyLlmModels: build.query<{ items: LlmModelInfo[] }, void>({
      query: () => '/v1/llm/me/models',
      providesTags: ['LLMMyModels']
    }),
    // Records the outcome on the credential, so the list is refetched.
    testLlmCredential: build.mutation<LlmCredentialTestResult, string>({
      query: uuid => ({
        url: `/v1/admin/llm/credentials/${uuid}/test`,
        method: 'POST'
      }),
      invalidatesTags: ['LLMCredential']
    }),
    // A POST that changes nothing: a mutation so it runs only on demand.
    discoverLlmModels: build.mutation<{ items: LlmDiscoveredModel[] }, string>({
      query: uuid => ({
        url: `/v1/admin/llm/credentials/${uuid}/models`,
        method: 'POST'
      })
    }),
    // Reserved against the budget and ledgered as llm.admin_test.
    promptTestLlmModel: build.mutation<
      LlmPromptTestResult,
      { uuid: string; prompt: string }
    >({
      query: ({ uuid, prompt }) => ({
        url: `/v1/admin/llm/models/${uuid}/prompt`,
        method: 'POST',
        body: { prompt }
      }),
      invalidatesTags: ['LLMUsage']
    }),
    getLlmUsage: build.query<LlmUsageReport, LlmUsageQuery>({
      query: ({ from, to, groupBy }) => ({
        url: '/v1/admin/llm/usage',
        params: {
          groupBy,
          ...(from ? { from } : {}),
          ...(to ? { to } : {})
        }
      }),
      providesTags: ['LLMUsage']
    }),
    // 204 (no budget, i.e. no limit) arrives as an empty body → null.
    // The arg is the active org id. The URL is the same for every org (the
    // tenant header selects it): the arg only keys the cache per org.
    getLlmBudget: build.query<LlmBudget | null, string>({
      query: () => '/v1/admin/llm/budget',
      transformResponse: (body: LlmBudget | null | undefined) => body ?? null,
      providesTags: ['LLMBudget']
    }),
    putLlmBudget: build.mutation<LlmBudget, LlmBudgetBody>({
      query: body => ({ url: '/v1/admin/llm/budget', method: 'PUT', body }),
      invalidatesTags: ['LLMBudget', 'LLMUsage']
    }),
    getMyLlmUsage: build.query<LlmSelfUsage, void>({
      query: () => '/v1/llm/me/usage',
      providesTags: ['LLMUsage']
    })
  })
});

export const {
  useListLlmCredentialsQuery,
  useCreateLlmCredentialMutation,
  usePatchLlmCredentialMutation,
  useRotateLlmCredentialMutation,
  useDeleteLlmCredentialMutation,
  useListLlmModelsQuery,
  useCreateLlmModelMutation,
  usePatchLlmModelMutation,
  useDeleteLlmModelMutation,
  usePutLlmGrantsMutation,
  useListMyLlmModelsQuery,
  useTestLlmCredentialMutation,
  useDiscoverLlmModelsMutation,
  usePromptTestLlmModelMutation,
  useGetLlmUsageQuery,
  useGetLlmBudgetQuery,
  usePutLlmBudgetMutation,
  useGetMyLlmUsageQuery
} = llmApi;
