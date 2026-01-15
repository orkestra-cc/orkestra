import { baseApi } from './baseApi';

// OAuth Provider information
export interface UserOAuthProviderInfo {
  provider: string;
  email: string;
  avatar?: string;
}

// Medical check record
export interface MedicalCheck {
  id: string;
  type: string;
  notes?: string;
  expiry?: string;
  booked?: string;
  where?: string;
  doctor?: string;
}

// User management types based on backend OpenAPI
export interface User {
  id: string;
  email: string;
  username: string;
  fullName: string;
  avatar?: string;
  role: string;
  phone?: string;
  providers: UserOAuthProviderInfo[];
  licenseNumber?: string;
  licenseExpiry?: string;
  driverCardNumber?: string;
  driverCardExpiry?: string;
  cqcExpiry?: string;
  adrNumber?: string;
  adrExpiry?: string;
  tachigrafExpiry?: string;
  medicalChecks?: MedicalCheck[];
  isActive: boolean;
  emailVerified: boolean;
  lastLogin?: string;
  createdAt: string;
  updatedAt: string;
}

export interface UserListResponse {
  users: User[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface CreateUserInput {
  email: string;
  username: string;
  fullName: string;
  phone: string;
  pin: string;
  role: string;
  licenseNumber?: string;
  licenseExpiry?: string;
  driverCardNumber?: string;
  driverCardExpiry?: string;
  cqcExpiry?: string;
  adrNumber?: string;
  adrExpiry?: string;
  tachigrafExpiry?: string;
  medicalChecks?: MedicalCheck[];
}

export interface UpdateUserInput {
  email?: string;
  username?: string;
  fullName?: string;
  phone?: string;
  role?: string;
  isActive?: boolean;
  pin?: string;
  avatar?: string;
  licenseNumber?: string;
  licenseExpiry?: string;
  driverCardNumber?: string;
  driverCardExpiry?: string;
  cqcExpiry?: string;
  adrNumber?: string;
  adrExpiry?: string;
  tachigrafExpiry?: string;
  medicalChecks?: MedicalCheck[];
}

export interface DeleteUserResponse {
  success: boolean;
  message: string;
}

export interface UserActivity {
  id: string;
  type: 'login' | 'profile' | 'security' | 'task' | 'permission';
  action: string;
  timestamp: string;
  ipAddress: string;
  device: string;
  status: 'success' | 'warning' | 'info' | 'danger';
}

export interface UserActivitiesResponse {
  activities: UserActivity[];
  total: number;
  page: number;
  pageSize: number;
}

export interface UserMetrics {
  tasksCompleted: number;
  totalTasks: number;
  onTimeDeliveryRate: number;
  performanceRating: number;
  teamCollaboration: number;
  systemUsage: {
    dashboard: number;
    fleetManagement: number;
    reports: number;
    settings: number;
    helpDesk: number;
  };
  quickStats: {
    loginCount: number;
    onlineTimeHours: number;
    activeTasks: number;
    overdueTasks: number;
  };
}

export interface UserListParams {
  role?: string;
  isActive?: boolean;
  emailVerified?: boolean;
  search?: string;
  hasExpiredDocs?: boolean;
  page?: number;
  pageSize?: number;
}

// User management API slice
export const userApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // List users with filtering and pagination
    getUsers: builder.query<UserListResponse, UserListParams | undefined>({
      query: (params) => {
        const searchParams = new URLSearchParams();

        // Add parameters if they exist
        if (params?.role) searchParams.append('role', params.role);
        if (params?.isActive !== undefined) searchParams.append('isActive', String(params.isActive));
        if (params?.emailVerified !== undefined) searchParams.append('emailVerified', String(params.emailVerified));
        if (params?.search) searchParams.append('search', params.search);
        if (params?.hasExpiredDocs !== undefined) searchParams.append('hasExpiredDocs', String(params.hasExpiredDocs));
        if (params?.page !== undefined) searchParams.append('page', String(params.page));
        if (params?.pageSize !== undefined) searchParams.append('pageSize', String(params.pageSize));

        return {
          url: `/api/v1/users?${searchParams.toString()}`,
          method: 'GET',
        };
      },
      providesTags: (result) =>
        result
          ? [
              ...result.users.map(({ id }) => ({ type: 'User' as const, id })),
              { type: 'User', id: 'LIST' },
            ]
          : [{ type: 'User', id: 'LIST' }],
    }),

    // Get user by ID
    getUserById: builder.query<User, string>({
      query: (id) => `/api/v1/users/${id}`,
      providesTags: (_result, _error, id) => [{ type: 'User', id }],
    }),

    // Get user by email
    getUserByEmail: builder.query<User, string>({
      query: (email) => `/api/v1/users/by-email?email=${email}`,
      providesTags: (result) => result ? [{ type: 'User', id: result.id }] : [],
    }),

    // Create new user
    createUser: builder.mutation<User, CreateUserInput>({
      query: (userData) => ({
        url: '/api/v1/users',
        method: 'POST',
        body: userData,
      }),
      invalidatesTags: [{ type: 'User', id: 'LIST' }],
    }),

    // Update user
    updateUser: builder.mutation<User, { id: string; data: UpdateUserInput }>({
      query: ({ id, data }) => ({
        url: `/api/v1/users/${id}`,
        method: 'PUT',
        body: data,
      }),
      invalidatesTags: (_result, _error, { id }) => [
        { type: 'User', id },
        { type: 'User', id: 'LIST' },
      ],
    }),

    // Delete user
    deleteUser: builder.mutation<DeleteUserResponse, string>({
      query: (id) => ({
        url: `/api/v1/users/${id}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, id) => [
        { type: 'User', id },
        { type: 'User', id: 'LIST' },
      ],
    }),

    // Get user activities
    getUserActivities: builder.query<UserActivitiesResponse, { userId: string; page?: number; pageSize?: number; type?: string }>({
      query: ({ userId, page = 1, pageSize = 10, type }) => {
        const searchParams = new URLSearchParams();
        searchParams.append('page', String(page));
        searchParams.append('pageSize', String(pageSize));
        if (type) searchParams.append('type', type);

        return `/api/v1/users/${userId}/activities?${searchParams.toString()}`;
      },
      providesTags: (_result, _error, { userId }) => [
        { type: 'User' as const, id: `activities-${userId}` }
      ],
    }),

    // Get user metrics
    getUserMetrics: builder.query<UserMetrics, string>({
      query: (userId) => `/api/v1/users/${userId}/metrics`,
      providesTags: (_result, _error, userId) => [
        { type: 'User' as const, id: `metrics-${userId}` }
      ],
    }),
  }),
});

// Export hooks for usage in components
export const {
  useGetUsersQuery,
  useGetUserByIdQuery,
  useGetUserByEmailQuery,
  useCreateUserMutation,
  useUpdateUserMutation,
  useDeleteUserMutation,
  useGetUserActivitiesQuery,
  useGetUserMetricsQuery,
} = userApi;