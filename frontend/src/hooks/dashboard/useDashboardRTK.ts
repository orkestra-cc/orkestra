import { useCallback } from 'react';
import {
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
  type DashboardType,
  type TimeRange
} from '../../store/api/dashboardApi';

/**
 * Individual dashboard data hooks using RTK Query
 * These replace the original TanStack Query hooks
 */

export const useDashboardStats = (dashboardType: DashboardType = 'default') => {
  return useGetDashboardStatsQuery({ dashboardType });
};

export const useWeatherData = (location: string = 'New York') => {
  return useGetWeatherDataQuery({ location });
};

export const useTotalSales = (timeRange: TimeRange = '30d') => {
  return useGetTotalSalesQuery({ timeRange });
};

export const useTotalOrders = (timeRange: TimeRange = '30d') => {
  return useGetTotalOrdersQuery({ timeRange });
};

export const useActiveUsers = (timeRange: TimeRange = '7d') => {
  return useGetActiveUsersQuery({ timeRange });
};

export const useWeeklySales = (weeks: number = 12) => {
  return useGetWeeklySalesQuery({ weeks });
};

export const useBestSellingProducts = (limit: number = 10) => {
  return useGetBestSellingProductsQuery({ limit });
};

export const useMarketShare = () => {
  return useGetMarketShareQuery();
};

export const useRunningProjects = () => {
  return useGetRunningProjectsQuery();
};

export const useStorageStatus = () => {
  return useGetStorageStatusQuery();
};

/**
 * Combined dashboard hooks that fetch multiple data sources
 * These replace the useQueries patterns from the original implementation
 */

export const useDefaultDashboard = (timeRange: TimeRange = '30d') => {
  const salesQuery = useGetTotalSalesQuery({ timeRange });
  const ordersQuery = useGetTotalOrdersQuery({ timeRange });
  const activeUsersQuery = useGetActiveUsersQuery({ timeRange: '7d' });
  const weeklySalesQuery = useGetWeeklySalesQuery({ weeks: 12 });
  const bestProductsQuery = useGetBestSellingProductsQuery({ limit: 5 });
  const storageQuery = useGetStorageStatusQuery();
  const weatherQuery = useGetWeatherDataQuery({ location: 'New York' });

  // Compute combined loading state
  const isLoading = [
    salesQuery,
    ordersQuery,
    activeUsersQuery,
    weeklySalesQuery,
    bestProductsQuery,
    storageQuery,
    weatherQuery
  ].some(query => query.isLoading);

  // Compute combined error state
  const hasError = [
    salesQuery,
    ordersQuery,
    activeUsersQuery,
    weeklySalesQuery,
    bestProductsQuery,
    storageQuery,
    weatherQuery
  ].some(query => query.error);

  // Refetch all queries
  const refetch = useCallback(() => {
    return Promise.all([
      salesQuery.refetch(),
      ordersQuery.refetch(),
      activeUsersQuery.refetch(),
      weeklySalesQuery.refetch(),
      bestProductsQuery.refetch(),
      storageQuery.refetch(),
      weatherQuery.refetch()
    ]);
  }, [
    salesQuery.refetch,
    ordersQuery.refetch,
    activeUsersQuery.refetch,
    weeklySalesQuery.refetch,
    bestProductsQuery.refetch,
    storageQuery.refetch,
    weatherQuery.refetch
  ]);

  return {
    // Individual query results (matching original useQueries format)
    queries: [
      salesQuery,
      ordersQuery,
      activeUsersQuery,
      weeklySalesQuery,
      bestProductsQuery,
      storageQuery,
      weatherQuery
    ],
    // Named results for easy access
    data: {
      sales: salesQuery.data,
      orders: ordersQuery.data,
      activeUsers: activeUsersQuery.data,
      weeklySales: weeklySalesQuery.data,
      bestProducts: bestProductsQuery.data,
      storage: storageQuery.data,
      weather: weatherQuery.data
    },
    // Combined states
    isLoading,
    hasError,
    refetch
  };
};

export const useAnalyticsDashboard = (timeRange: TimeRange = '30d') => {
  const dashboardStatsQuery = useGetDashboardStatsQuery({ dashboardType: 'analytics' });
  const marketShareQuery = useGetMarketShareQuery();
  const activeUsersQuery = useGetActiveUsersQuery({ timeRange });

  const isLoading = [
    dashboardStatsQuery,
    marketShareQuery,
    activeUsersQuery
  ].some(query => query.isLoading);

  const hasError = [
    dashboardStatsQuery,
    marketShareQuery,
    activeUsersQuery
  ].some(query => query.error);

  const refetch = useCallback(() => {
    return Promise.all([
      dashboardStatsQuery.refetch(),
      marketShareQuery.refetch(),
      activeUsersQuery.refetch()
    ]);
  }, [
    dashboardStatsQuery.refetch,
    marketShareQuery.refetch,
    activeUsersQuery.refetch
  ]);

  return {
    queries: [
      dashboardStatsQuery,
      marketShareQuery,
      activeUsersQuery
    ],
    data: {
      overview: dashboardStatsQuery.data,
      marketShare: marketShareQuery.data,
      activeUsers: activeUsersQuery.data
    },
    isLoading,
    hasError,
    refetch
  };
};

export const useCrmDashboard = () => {
  const dashboardStatsQuery = useGetDashboardStatsQuery({ dashboardType: 'crm' });
  const salesQuery = useGetTotalSalesQuery({ timeRange: '30d' });
  const projectsQuery = useGetRunningProjectsQuery();

  const isLoading = [
    dashboardStatsQuery,
    salesQuery,
    projectsQuery
  ].some(query => query.isLoading);

  const hasError = [
    dashboardStatsQuery,
    salesQuery,
    projectsQuery
  ].some(query => query.error);

  const refetch = useCallback(() => {
    return Promise.all([
      dashboardStatsQuery.refetch(),
      salesQuery.refetch(),
      projectsQuery.refetch()
    ]);
  }, [
    dashboardStatsQuery.refetch,
    salesQuery.refetch,
    projectsQuery.refetch
  ]);

  return {
    queries: [
      dashboardStatsQuery,
      salesQuery,
      projectsQuery
    ],
    data: {
      stats: dashboardStatsQuery.data,
      sales: salesQuery.data,
      projects: projectsQuery.data
    },
    isLoading,
    hasError,
    refetch
  };
};

export const useProjectManagementDashboard = () => {
  const projectsQuery = useGetRunningProjectsQuery();
  const activeUsersQuery = useGetActiveUsersQuery({ timeRange: '7d' });
  const dashboardStatsQuery = useGetDashboardStatsQuery({ dashboardType: 'project-management' });

  const isLoading = [
    projectsQuery,
    activeUsersQuery,
    dashboardStatsQuery
  ].some(query => query.isLoading);

  const hasError = [
    projectsQuery,
    activeUsersQuery,
    dashboardStatsQuery
  ].some(query => query.error);

  const refetch = useCallback(() => {
    return Promise.all([
      projectsQuery.refetch(),
      activeUsersQuery.refetch(),
      dashboardStatsQuery.refetch()
    ]);
  }, [
    projectsQuery.refetch,
    activeUsersQuery.refetch,
    dashboardStatsQuery.refetch
  ]);

  return {
    queries: [
      projectsQuery,
      activeUsersQuery,
      dashboardStatsQuery
    ],
    data: {
      projects: projectsQuery.data,
      activeUsers: activeUsersQuery.data,
      stats: dashboardStatsQuery.data
    },
    isLoading,
    hasError,
    refetch
  };
};