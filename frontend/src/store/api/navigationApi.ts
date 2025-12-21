import { baseApi } from './baseApi';

// Types matching backend response
export interface Badge {
  type: string;
  text: string;
}

export interface NavItem {
  name: string;
  to?: string;
  icon?: string | string[];
  active?: boolean;
  exact?: boolean;
  newtab?: boolean;
  badge?: Badge;
  label?: string;
  children?: NavItem[];
}

export interface RouteGroup {
  label: string;
  labelDisable?: boolean;
  children: NavItem[];
}

export interface NavigationResponse {
  groups: RouteGroup[];
  userRole: string;
  cacheKey: string;
  expiresIn: number;
}

// Navigation API slice
export const navigationApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Get navigation menu (role-filtered by backend)
    getNavigation: builder.query<NavigationResponse | null, void>({
      providesTags: ['Navigation'],
      queryFn: async (_arg, _api, _extraOptions, baseQuery) => {
        const result = await baseQuery('api/v1/navigation');

        // Handle authentication errors - return null instead of error
        if (result.error && (result.error.status === 401 || result.error.status === 403)) {
          return { data: null };
        }

        if (result.error) {
          return { error: result.error };
        }

        // Navigation data is returned directly (not wrapped in body)
        const navigationData = result.data as NavigationResponse;
        return { data: navigationData ?? null };
      },
      // Cache for 5 minutes (matches backend expiresIn)
      keepUnusedDataFor: 300,
    }),
  }),
});

// Export hooks
export const {
  useGetNavigationQuery,
  useLazyGetNavigationQuery,
} = navigationApi;
