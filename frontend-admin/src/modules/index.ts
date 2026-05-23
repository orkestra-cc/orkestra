import type { ModuleManifest } from './types';
import { billingManifest } from './billing';
import { companyManifest } from './company';
import { graphManifest } from './graph';
import { aimodelsManifest } from './aimodels';
import { ragManifest } from './rag';
import { agentsManifest } from './agents';
import { salesManifest } from './sales';
import { subscriptionsManifest } from './subscriptions';
import { paymentsManifest } from './payments';
import { complianceManifest } from './compliance';
import { identityManifest } from './identity';
import { marketingManifest } from './marketing';

/** All optional module manifests, keyed by backend module name */
export const moduleCatalog: Record<string, ModuleManifest> = {
  billing: billingManifest,
  company: companyManifest,
  graph: graphManifest,
  aimodels: aimodelsManifest,
  rag: ragManifest,
  agents: agentsManifest,
  sales: salesManifest,
  subscriptions: subscriptionsManifest,
  payments: paymentsManifest,
  compliance: complianceManifest,
  identity: identityManifest,
  marketing: marketingManifest
};

export type { ModuleManifest } from './types';
