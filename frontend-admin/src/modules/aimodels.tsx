import type { ModuleManifest } from './types';

export const aimodelsManifest: ModuleManifest = {
  name: 'aimodels',
  routes: () => [],
  injectApi: () => import('store/api/aiModelsApi'),
};
