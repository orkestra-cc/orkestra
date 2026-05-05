import { Suspense, lazy } from 'react';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import FalconLoader from 'components/common/FalconLoader';

const AgentProjects = lazy(() => import('pages/ai/agents'));
const AgentChat = lazy(() => import('pages/ai/agents/AgentChat'));
const PersonalAgentChat = lazy(() => import('pages/ai/personal-agent/PersonalAgentChat'));

export const agentsManifest: ModuleManifest = {
  name: 'agents',
  routes: () => [
    {
      path: 'ai/personal-agent',
      element: (
        <ModuleGate module="agents">
          <ProtectedRoute requiredPermissions={[['super_admin', 'administrator', 'developer', 'manager', 'operator', 'guest']]}>
            <Suspense key="ai-personal-agent" fallback={<FalconLoader />}>
              <PersonalAgentChat />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'ai/agents',
      element: (
        <ModuleGate module="agents">
          <ProtectedRoute requiredPermissions={[['super_admin', 'administrator', 'developer', 'manager']]}>
            <Suspense key="ai-agents" fallback={<FalconLoader />}>
              <AgentProjects />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'ai/agents/:uuid/chat',
      element: (
        <ModuleGate module="agents">
          <ProtectedRoute requiredPermissions={[['super_admin', 'administrator', 'developer', 'manager', 'operator']]}>
            <Suspense key="ai-agent-chat" fallback={<FalconLoader />}>
              <AgentChat />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
  ],
  injectApi: () =>
    Promise.all([
      import('store/api/agentsApi'),
      import('store/api/personalAgentApi'),
      import('store/api/ragApi'),
    ]),
};
