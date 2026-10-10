import { baseApi } from './baseApi';

// llmApi wraps the core llm module (ADR-0022): credentials (secrets are
// write-only — the API returns only hasSecret/secretLast4), configured
// models with their per-user grants, and the caller's own usable models.
// Credential create/patch/rotate/delete and grant replacement are
// step-up-gated on the backend; baseApi's 401 interceptor drives the
// StepUpModal and replays the request, so nothing here handles it.
// Shapes mirror backend/internal/core/llm/models (dto.go, credential.go,
// model.go, grant.go).

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
  useListMyLlmModelsQuery
} = llmApi;
