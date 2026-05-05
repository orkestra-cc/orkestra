import { baseApi } from './baseApi';

// --- Types ---

export interface ConfigField {
  key: string;
  label: string;
  group?: string;
  description: string;
  type: 'string' | 'bool' | 'int' | 'duration' | 'secret';
  required: boolean;
  default: string;
  envVar: string;
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
  endpoints: (builder) => ({
    getModules: builder.query<ModuleConfig[], void>({
      query: () => '/v1/admin/modules',
      transformResponse: (response: ListModulesResponse) => response.modules,
      providesTags: (result) =>
        result
          ? [
              ...result.map(({ moduleName }) => ({
                type: 'Module' as const,
                id: moduleName,
              })),
              { type: 'Module', id: 'LIST' },
            ]
          : [{ type: 'Module', id: 'LIST' }],
    }),

    getModule: builder.query<ModuleConfig, string>({
      query: (name) => `/v1/admin/modules/${name}`,
      providesTags: (_result, _error, name) => [{ type: 'Module', id: name }],
    }),

    updateModule: builder.mutation<ModuleConfig, UpdateModuleParams>({
      query: ({ name, ...body }) => ({
        url: `/v1/admin/modules/${name}`,
        method: 'PATCH',
        body,
      }),
      invalidatesTags: (_result, _error, { name }) => [
        { type: 'Module', id: name },
        { type: 'Module', id: 'LIST' },
        'ModuleHealth',
        'Navigation',
      ],
    }),

    getModulesHealth: builder.query<HealthResponse, void>({
      query: () => '/v1/admin/modules/health',
      providesTags: ['ModuleHealth'],
      keepUnusedDataFor: 30,
    }),

    getModuleEnvironment: builder.query<
      EnvironmentConfigResponse,
      { name: string; environment: string }
    >({
      query: ({ name, environment }) =>
        `/v1/admin/modules/${name}/environments/${environment}`,
      providesTags: (_result, _error, { name, environment }) => [
        { type: 'Module', id: `${name}-env-${environment}` },
      ],
    }),

    updateModuleEnvironment: builder.mutation<
      EnvironmentConfigResponse,
      UpdateEnvironmentParams
    >({
      query: ({ name, environment, ...body }) => ({
        url: `/v1/admin/modules/${name}/environments/${environment}`,
        method: 'PATCH',
        body,
      }),
      invalidatesTags: (_result, _error, { name, environment }) => [
        { type: 'Module', id: name },
        { type: 'Module', id: `${name}-env-${environment}` },
        { type: 'Module', id: 'LIST' },
        'ModuleHealth',
      ],
    }),

    setActiveEnvironment: builder.mutation<
      { activeEnvironment: string; needsRestart: boolean },
      SetActiveEnvironmentParams
    >({
      query: ({ name, environment }) => ({
        url: `/v1/admin/modules/${name}/active-environment`,
        method: 'PUT',
        body: { environment },
      }),
      invalidatesTags: (_result, _error, { name }) => [
        { type: 'Module', id: name },
        { type: 'Module', id: 'LIST' },
        'ModuleHealth',
      ],
    }),
  }),
});

export const {
  useGetModulesQuery,
  useGetModuleQuery,
  useUpdateModuleMutation,
  useGetModulesHealthQuery,
  useGetModuleEnvironmentQuery,
  useUpdateModuleEnvironmentMutation,
  useSetActiveEnvironmentMutation,
} = moduleApi;
