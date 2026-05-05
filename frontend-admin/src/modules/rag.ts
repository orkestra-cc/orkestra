import type { ModuleManifest } from './types';

export const ragManifest: ModuleManifest = {
  name: 'rag',
  routes: () => [],
  injectApi: () => import('store/api/ragApi'),
};
