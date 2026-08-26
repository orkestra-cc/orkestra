import { Suspense, useMemo } from 'react';
import { useRoleBasedNavigation } from 'hooks/useRoleBasedNavigation';
import type { NavItem, NavRealm, RouteGroup } from 'store/api/navigationApi';
import { moduleCatalog } from '../index';

const collectModules = (items: NavItem[], result: Set<string>) => {
  for (const item of items) {
    if (item.moduleName) result.add(item.moduleName);
    if (item.children) collectModules(item.children, result);
  }
};

export const collectVisibleModuleNames = (
  groups: RouteGroup[],
  realms: NavRealm[]
) => {
  const result = new Set<string>();
  for (const group of groups) collectModules(group.children, result);
  for (const realm of realms) {
    for (const section of realm.sections) {
      collectModules(section.children, result);
    }
  }
  return result;
};

/** Mounts addon-owned global surfaces without importing addons from core UI. */
const ModuleGlobalOverlays = () => {
  const { filteredNavigation, realms, isAuthenticated, isLoading, isError } =
    useRoleBasedNavigation();

  const visibleModules = useMemo(() => {
    return collectVisibleModuleNames(filteredNavigation, realms);
  }, [filteredNavigation, realms]);

  if (!isAuthenticated || isLoading || isError) return null;

  return Object.values(moduleCatalog).map(manifest => {
    const Overlay = manifest.globalOverlay;
    if (!Overlay || !visibleModules.has(manifest.name)) return null;

    return (
      <Suspense key={manifest.name} fallback={null}>
        <Overlay />
      </Suspense>
    );
  });
};

export default ModuleGlobalOverlays;
