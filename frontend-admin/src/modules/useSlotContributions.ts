import { useMemo } from 'react';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { moduleCatalog } from './index';
import {
  collectSlotContributions,
  type ResolvedSlotContribution
} from './slotContributions';
import { useActiveModuleNames } from './useActiveModuleNames';

/**
 * The contributions to `slotId` the current user should see: active modules
 * only (see useActiveModuleNames), filtered by each contribution's permission.
 */
export function useSlotContributions(
  slotId: string
): ResolvedSlotContribution[] {
  const active = useActiveModuleNames();
  const { hasPermission } = useAuth();

  return useMemo(
    () =>
      collectSlotContributions(moduleCatalog, slotId, active, hasPermission),
    [active, hasPermission, slotId]
  );
}
