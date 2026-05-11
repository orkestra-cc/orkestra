import { useQuery } from '@tanstack/react-query';
import { getMe, type MeResponse } from '@/api/auth';
import { useAuth } from '@/auth/useAuth';

// Hook for the authenticated user profile. Disabled when no access
// token is in memory so anonymous pages don't fire a guaranteed-401.
// Cache stays fresh for 60s — the profile rarely changes mid-session
// and re-fetches happen on window focus only when explicitly enabled.
export function useMe() {
  const { isAuthenticated } = useAuth();
  return useQuery<MeResponse>({
    queryKey: ['me'],
    queryFn: ({ signal }) => getMe(signal),
    enabled: isAuthenticated,
    staleTime: 60_000,
    retry: false,
  });
}
