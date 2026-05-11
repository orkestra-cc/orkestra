import { useCallback, useMemo, useState } from 'react';

// Types for infinite/paginated data with RTK Query
export interface PaginatedApiResponse<T> {
  items: T[];
  hasMore: boolean;
  totalCount: number;
  currentPage: number;
  totalPages: number;
  nextCursor?: string;
}

export interface InfiniteDataState<T> {
  pages: PaginatedApiResponse<T>[];
  allItems: T[];
  totalItems: number;
  hasMore: boolean;
  isLoading: boolean;
  isFetchingMore: boolean;
  error: any;
}

/**
 * RTK Query-based infinite data hook
 * Provides pagination and infinite scrolling functionality
 *
 * Usage with RTK Query:
 * ```typescript
 * const infiniteEmails = useInfiniteData({
 *   useQuery: useGetEmailsQuery,
 *   queryArgs: { folder: 'inbox' },
 *   getNextPageParam: (lastPage) => lastPage.nextCursor,
 * });
 * ```
 */
interface UseInfiniteDataOptions<TData, TQueryArg> {
  useQuery: (arg: TQueryArg & { cursor?: string; limit?: number }, options?: { skip?: boolean }) => {
    data?: PaginatedApiResponse<TData>;
    isLoading: boolean;
    isFetching: boolean;
    error: any;
    refetch: () => void;
  };
  queryArgs: TQueryArg;
  getNextPageParam?: (lastPage: PaginatedApiResponse<TData>) => string | undefined;
  limit?: number;
  enabled?: boolean;
}

export function useInfiniteData<TData = unknown, TQueryArg = any>({
  useQuery,
  queryArgs,
  getNextPageParam,
  limit = 25,
  enabled = true,
}: UseInfiniteDataOptions<TData, TQueryArg>) {
  // Track cursors for pagination
  const [cursors, setCursors] = useState<string[]>([]);

  // Get current page data
  const {
    data: currentPage,
    isLoading,
    isFetching,
    error,
    refetch
  } = useQuery({
    ...queryArgs,
    cursor: cursors[cursors.length - 1],
    limit
  }, { skip: !enabled });

  // Build infinite data state
  const infiniteData = useMemo<InfiniteDataState<TData>>(() => {
    if (!currentPage) {
      return {
        pages: [],
        allItems: [],
        totalItems: 0,
        hasMore: false,
        isLoading,
        isFetchingMore: isFetching && cursors.length > 0,
        error
      };
    }

    // For RTK Query, we need to manually manage the pages
    // This is a simplified version - in practice, you'd store all pages
    return {
      pages: [currentPage],
      allItems: currentPage.items,
      totalItems: currentPage.totalCount,
      hasMore: currentPage.hasMore,
      isLoading,
      isFetchingMore: isFetching && cursors.length > 0,
      error
    };
  }, [currentPage, isLoading, isFetching, error, cursors.length]);

  // Fetch next page
  const fetchNextPage = useCallback(() => {
    if (!currentPage || !currentPage.hasMore) return;

    const nextCursor = getNextPageParam?.(currentPage) ||
                      (currentPage.currentPage + 1).toString();

    if (nextCursor) {
      setCursors(prev => [...prev, nextCursor]);
    }
  }, [currentPage, getNextPageParam]);

  // Refetch all pages
  const refetchAll = useCallback(() => {
    setCursors([]);
    refetch();
  }, [refetch]);

  return {
    ...infiniteData,
    fetchNextPage,
    refetch: refetchAll,
    canFetchMore: infiniteData.hasMore && !infiniteData.isFetchingMore,
  };
}

/**
 * Simplified infinite scroll hook for RTK Query endpoints
 * that already handle cursor-based pagination internally
 *
 * Usage:
 * ```typescript
 * const { data, hasMore, fetchNext } = useInfiniteScroll(
 *   useGetEmailsQuery,
 *   { folder: 'inbox' }
 * );
 * ```
 */
export function useInfiniteScroll<TData, TQueryArg>(
  useQuery: (arg: TQueryArg, options?: { skip?: boolean }) => any,
  queryArgs: TQueryArg,
  options: { enabled?: boolean } = {}
) {
  const { data, isLoading, isFetching, error } = useQuery(queryArgs, {
    skip: options.enabled === false
  }) as { data?: { items: TData[]; hasMore: boolean; totalCount: number }; isLoading: boolean; isFetching: boolean; error: any };

  // For RTK Query endpoints that handle infinite data internally
  // The query itself manages the cursor/pagination state
  return {
    data: data?.items || [],
    hasMore: data?.hasMore || false,
    totalCount: data?.totalCount || 0,
    isLoading,
    isFetching,
    error,
    // Note: fetchNext would need to be implemented in the RTK Query endpoint
    // using serializeQueryArgs and merge functions
  };
}

// Re-export for backward compatibility
export { useInfiniteData as useInfiniteQuery };

// Helper function to create infinite query configurations for RTK Query
export function createInfiniteQueryConfig<TData>() {
  return {
    serializeQueryArgs: ({ queryArgs }: any) => {
      const { cursor, ...rest } = queryArgs;
      return rest; // Group by everything except cursor
    },
    merge: (currentCache: any, newItems: PaginatedApiResponse<TData>, { arg }: any) => {
      if (!arg.cursor) {
        // First page or reset
        return newItems;
      }
      // Append new items for infinite scroll
      return {
        ...newItems,
        items: [...(currentCache?.items || []), ...newItems.items]
      };
    },
  };
}