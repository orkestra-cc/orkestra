import { baseApi } from './baseApi';

// Types from the original dashboard hooks
export interface DashboardStats {
  totalUsers: number;
  activeUsers: number;
  totalRevenue: number;
  totalOrders: number;
  conversionRate: number;
  bounceRate: number;
  pageViews: number;
  sessionDuration: number;
}

export interface WeatherData {
  location: string;
  temperature: number;
  condition: string;
  humidity: number;
  windSpeed: number;
  icon: string;
  forecast: {
    date: string;
    high: number;
    low: number;
    condition: string;
    icon: string;
  }[];
}

export interface SalesData {
  total: number;
  growth: number;
  period: string;
  breakdown: {
    date: string;
    amount: number;
  }[];
}

export interface OrdersData {
  total: number;
  growth: number;
  period: string;
  statusBreakdown: {
    status: string;
    count: number;
    percentage: number;
  }[];
}

export interface ActiveUsersData {
  total: number;
  growth: number;
  period: string;
  breakdown: {
    date: string;
    count: number;
  }[];
}

export interface WeeklySalesData {
  weeks: number;
  data: {
    week: string;
    sales: number;
    growth: number;
  }[];
  totalSales: number;
  averageWeeklySales: number;
}

export interface BestSellingProduct {
  id: string | number;
  name: string;
  sales: number;
  revenue: number;
  imageUrl?: string;
  category: string;
}

export interface MarketShareData {
  segments: {
    name: string;
    percentage: number;
    value: number;
    color: string;
  }[];
  totalMarket: number;
  ourShare: number;
}

export interface RunningProject {
  id: string | number;
  name: string;
  progress: number;
  status: 'active' | 'paused' | 'completed' | 'delayed';
  startDate: string;
  endDate: string;
  team: {
    id: string | number;
    name: string;
    avatar?: string;
  }[];
  budget: number;
  spent: number;
}

export interface StorageStatus {
  used: number;
  total: number;
  percentage: number;
  breakdown: {
    type: string;
    size: number;
    percentage: number;
  }[];
}

export type DashboardType = 'default' | 'analytics' | 'crm' | 'ecommerce' | 'project-management' | 'saas' | 'support-desk';
export type TimeRange = '7d' | '30d' | '90d' | '1y';

// Dashboard API slice
export const dashboardApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Dashboard statistics
    getDashboardStats: builder.query<DashboardStats, { dashboardType?: DashboardType }>({
      query: ({ dashboardType = 'default' }) => `/dashboard/${dashboardType}/stats`,
      providesTags: (_result, _error, { dashboardType }) => [
        { type: 'Dashboard', id: `${dashboardType}-stats` }
      ],
      keepUnusedDataFor: 300, // 5 minutes
    }),

    // Weather data
    getWeatherData: builder.query<WeatherData, { location?: string }>({
      query: ({ location = 'New York' }) => `/weather?location=${encodeURIComponent(location)}`,
      providesTags: (_result, _error, { location }) => [
        { type: 'Weather', id: location }
      ],
      keepUnusedDataFor: 900, // 15 minutes
    }),

    // Sales data
    getTotalSales: builder.query<SalesData, { timeRange?: TimeRange }>({
      query: ({ timeRange = '30d' }) => `/sales/total?range=${timeRange}`,
      providesTags: (_result, _error, { timeRange }) => [
        { type: 'Sales', id: `total-${timeRange}` }
      ],
      keepUnusedDataFor: 300, // 5 minutes
    }),

    // Orders data
    getTotalOrders: builder.query<OrdersData, { timeRange?: TimeRange }>({
      query: ({ timeRange = '30d' }) => `/orders/total?range=${timeRange}`,
      providesTags: (_result, _error, { timeRange }) => [
        { type: 'Orders', id: `total-${timeRange}` }
      ],
      keepUnusedDataFor: 300, // 5 minutes
    }),

    // Active users
    getActiveUsers: builder.query<ActiveUsersData, { timeRange?: TimeRange }>({
      query: ({ timeRange = '7d' }) => `/users/active?range=${timeRange}`,
      providesTags: (_result, _error, { timeRange }) => [
        'User',
        { type: 'Analytics', id: `active-users-${timeRange}` }
      ],
      keepUnusedDataFor: 600, // 10 minutes
    }),

    // Weekly sales
    getWeeklySales: builder.query<WeeklySalesData, { weeks?: number }>({
      query: ({ weeks = 12 }) => `/sales/weekly?weeks=${weeks}`,
      providesTags: (_result, _error, { weeks }) => [
        { type: 'Sales', id: `weekly-${weeks}` }
      ],
      keepUnusedDataFor: 900, // 15 minutes
    }),

    // Best selling products
    getBestSellingProducts: builder.query<BestSellingProduct[], { limit?: number }>({
      query: ({ limit = 10 }) => `/products/best-selling?limit=${limit}`,
      providesTags: ['Sales', 'Analytics'],
      keepUnusedDataFor: 1800, // 30 minutes
    }),

    // Market share data
    getMarketShare: builder.query<MarketShareData, void>({
      query: () => '/analytics/market-share',
      providesTags: ['Analytics'],
      keepUnusedDataFor: 1800, // 30 minutes
    }),

    // Running projects
    getRunningProjects: builder.query<RunningProject[], void>({
      query: () => '/projects/running',
      providesTags: ['Projects'],
      keepUnusedDataFor: 300, // 5 minutes
    }),

    // Storage status
    getStorageStatus: builder.query<StorageStatus, void>({
      query: () => '/storage/status',
      providesTags: ['Storage'],
      keepUnusedDataFor: 300, // 5 minutes
    }),
  }),
});

// Export hooks
export const {
  useGetDashboardStatsQuery,
  useGetWeatherDataQuery,
  useGetTotalSalesQuery,
  useGetTotalOrdersQuery,
  useGetActiveUsersQuery,
  useGetWeeklySalesQuery,
  useGetBestSellingProductsQuery,
  useGetMarketShareQuery,
  useGetRunningProjectsQuery,
  useGetStorageStatusQuery,
  // Lazy query hooks
  useLazyGetDashboardStatsQuery,
  useLazyGetWeatherDataQuery,
  useLazyGetTotalSalesQuery,
  useLazyGetTotalOrdersQuery,
  useLazyGetActiveUsersQuery,
  useLazyGetWeeklySalesQuery,
  useLazyGetBestSellingProductsQuery,
  useLazyGetMarketShareQuery,
  useLazyGetRunningProjectsQuery,
  useLazyGetStorageStatusQuery,
} = dashboardApi;