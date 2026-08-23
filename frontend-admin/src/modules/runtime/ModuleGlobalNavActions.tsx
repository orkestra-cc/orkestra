import { Suspense, useMemo } from 'react';
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import { moduleCatalog } from '../index';
import { collectVisibleModuleNames } from './ModuleGlobalOverlays';

const ModuleGlobalNavActions = () => {
  const { filteredNavigation, realms, isAuthenticated, isLoading, isError } =
    useRoleBasedNavigation();
  const visibleModules = useMemo(
    () => collectVisibleModuleNames(filteredNavigation, realms),
    [filteredNavigation, realms]
  );

  if (!isAuthenticated || isLoading || isError) return null;

  return Object.values(moduleCatalog).map(manifest => {
    const Action = manifest.globalNavAction;
    if (!Action || !visibleModules.has(manifest.name)) return null;

    return (
      <Suspense key={manifest.name} fallback={null}>
        <Action />
      </Suspense>
    );
  });
};

export default ModuleGlobalNavActions;
