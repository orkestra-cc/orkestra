import type { ModuleManifest } from './types';

/**
 * All optional module manifests, keyed by backend module name.
 *
 * ADR-0006 collapsed Orkestra to a core-only base — the addon manifests
 * (billing, company, graph, aimodels, rag, agents, sales, subscriptions,
 * payments, compliance, identity, marketing) were removed. A fork that adds
 * its own optional modules registers their manifests here; see `_template/`
 * for the scaffold.
 */
export const moduleCatalog: Record<string, ModuleManifest> = {};

export type { ModuleManifest } from './types';
