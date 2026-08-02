import { baseApi } from './baseApi';

// --- Types ---

/**
 * Gates a field's visibility on the value of another field of the SAME module.
 * AND across a field's `dependsOn` array, OR within one condition's `in` list.
 *
 * Matching is type-aware, resolved against the *referenced* field's `type` —
 * this mirrors the contract documented on the backend's `FieldCondition`, and
 * the two implementations must not drift:
 *   - `bool` target: both sides are read as booleans, where `true` / `1` / `yes`
 *     (case-insensitive, trimmed) are true. So `in: ['true']` matches a stored `'1'`.
 *   - any other type: case-insensitive, whitespace-trimmed exact string match.
 */
export interface FieldCondition {
  key: string;
  in: string[];
}

/** One section of the settings rail. Presentation-only; never persisted. */
export interface ConfigGroup {
  key: string;
  label: string;
  description?: string;
  icon?: string;
  parent?: string;
  order?: number;
}

export interface ConfigField {
  key: string;
  label: string;
  group?: string;
  description: string;
  type:
    | 'string'
    | 'bool'
    | 'int'
    | 'duration'
    | 'secret'
    | 'enum'
    | 'stringList';
  required: boolean;
  default: string;
  envVar: string;
  options?: string[];
  advanced?: boolean;
  dependsOn?: FieldCondition[];
  min?: number;
  max?: number;
  pattern?: string;
  placeholder?: string;
  helpUrl?: string;
}

export interface InfraContainerStatus {
  name: string;
  image: string;
  running: boolean;
  error?: string;
}

export interface ModuleConfig {
  moduleName: string;
  displayName: string;
  description: string;
  category: 'core' | 'toggleable' | 'external';
  enabled: boolean;
  status: 'running' | 'failed' | 'disabled' | 'stopped';
  error?: string;
  needsRestart: boolean;
  configValues: Record<string, string>;
  secretStatus: Record<string, boolean>;
  configSchema: ConfigField[];
  configGroups?: ConfigGroup[];
  dependsOn: string[];
  providedServices: string[];
  requiredServices: string[];
  optionalServices: string[];
  infraContainers?: InfraContainerStatus[];
  activeEnvironment: string;
  availableEnvironments: string[];
  createdAt: string;
  updatedAt: string;
}

export interface EnvironmentConfigResponse {
  environment: string;
  configValues: Record<string, string>;
  secretStatus: Record<string, boolean>;
  updatedAt: string;
}

interface UpdateEnvironmentParams {
  name: string;
  environment: string;
  config?: Record<string, string>;
  secrets?: Record<string, string>;
}

interface SetActiveEnvironmentParams {
  name: string;
  environment: string;
}

export interface ModuleHealthStatus {
  moduleName: string;
  status: 'healthy' | 'unhealthy' | 'disabled' | 'failed';
  error?: string;
}

interface ListModulesResponse {
  modules: ModuleConfig[];
}

interface HealthResponse {
  modules: ModuleHealthStatus[];
  checkedAt: string;
}

interface UpdateModuleParams {
  name: string;
  enabled?: boolean;
  config?: Record<string, string>;
  secrets?: Record<string, string>;
}

// --- API Slice ---

export const moduleApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    getModules: builder.query<ModuleConfig[], void>({
      query: () => '/v1/admin/modules',
      transformResponse: (response: ListModulesResponse) => response.modules,
      providesTags: result =>
        result
          ? [
              ...result.map(({ moduleName }) => ({
                type: 'Module' as const,
                id: moduleName
              })),
              { type: 'Module', id: 'LIST' }
            ]
          : [{ type: 'Module', id: 'LIST' }]
    }),

    getModule: builder.query<ModuleConfig, string>({
      query: name => `/v1/admin/modules/${name}`,
      providesTags: (_result, _error, name) => [{ type: 'Module', id: name }]
    }),

    updateModule: builder.mutation<ModuleConfig, UpdateModuleParams>({
      query: ({ name, ...body }) => ({
        url: `/v1/admin/modules/${name}`,
        method: 'PATCH',
        body
      }),
      invalidatesTags: (_result, _error, { name }) => [
        { type: 'Module', id: name },
        { type: 'Module', id: 'LIST' },
        'ModuleHealth',
        'Navigation'
      ]
    }),

    getModulesHealth: builder.query<HealthResponse, void>({
      query: () => '/v1/admin/modules/health',
      providesTags: ['ModuleHealth'],
      keepUnusedDataFor: 30
    }),

    getModuleEnvironment: builder.query<
      EnvironmentConfigResponse,
      { name: string; environment: string }
    >({
      query: ({ name, environment }) =>
        `/v1/admin/modules/${name}/environments/${environment}`,
      providesTags: (_result, _error, { name, environment }) => [
        { type: 'Module', id: `${name}-env-${environment}` }
      ]
    }),

    updateModuleEnvironment: builder.mutation<
      EnvironmentConfigResponse,
      UpdateEnvironmentParams
    >({
      query: ({ name, environment, ...body }) => ({
        url: `/v1/admin/modules/${name}/environments/${environment}`,
        method: 'PATCH',
        body
      }),
      invalidatesTags: (_result, _error, { name, environment }) => [
        { type: 'Module', id: name },
        { type: 'Module', id: `${name}-env-${environment}` },
        { type: 'Module', id: 'LIST' },
        'ModuleHealth'
      ]
    }),

    setActiveEnvironment: builder.mutation<
      { activeEnvironment: string; needsRestart: boolean },
      SetActiveEnvironmentParams
    >({
      query: ({ name, environment }) => ({
        url: `/v1/admin/modules/${name}/active-environment`,
        method: 'PUT',
        body: { environment }
      }),
      invalidatesTags: (_result, _error, { name }) => [
        { type: 'Module', id: name },
        { type: 'Module', id: 'LIST' },
        'ModuleHealth'
      ]
    })
  })
});

export const {
  useGetModulesQuery,
  useGetModuleQuery,
  useUpdateModuleMutation,
  useGetModulesHealthQuery,
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation,
  useSetActiveEnvironmentMutation
} = moduleApi;
