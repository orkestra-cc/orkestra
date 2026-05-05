import { Suspense, lazy } from 'react';
import { Navigate } from 'react-router';
import type { ModuleManifest } from './types';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
import ModuleGate from 'components/common/ModuleGate';
import FalconLoader from 'components/common/FalconLoader';

const GraphExplorer = lazy(() => import('pages/graph/explorer'));
const GraphAlgorithms = lazy(() => import('pages/graph/algorithms'));
const GraphDatabases = lazy(() => import('pages/graph/databases'));
const GraphVector = lazy(() => import('pages/graph/vector'));
const GraphDocuments = lazy(() => import('pages/graph/documents'));
const GraphRelationships = lazy(() => import('pages/graph/relationships'));
const GraphRAG = lazy(() => import('pages/graph/rag'));

const graphPerms: [string[]] = [['super_admin', 'administrator', 'developer']];

export const graphManifest: ModuleManifest = {
  name: 'graph',
  routes: () => [
    {
      path: 'graph/explorer',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-explorer" fallback={<FalconLoader />}>
              <GraphExplorer />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'graph/algorithms',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-algorithms" fallback={<FalconLoader />}>
              <GraphAlgorithms />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'graph/databases',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-databases" fallback={<FalconLoader />}>
              <GraphDatabases />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'graph/vector',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-vector" fallback={<FalconLoader />}>
              <GraphVector />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'graph/models',
      element: <Navigate to="/ai/models" replace />,
    },
    {
      path: 'graph/documents',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-documents" fallback={<FalconLoader />}>
              <GraphDocuments />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'graph/relationships',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-relationships" fallback={<FalconLoader />}>
              <GraphRelationships />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
    {
      path: 'graph/rag',
      element: (
        <ModuleGate module="graph">
          <ProtectedRoute requiredPermissions={graphPerms}>
            <Suspense key="graph-rag" fallback={<FalconLoader />}>
              <GraphRAG />
            </Suspense>
          </ProtectedRoute>
        </ModuleGate>
      ),
    },
  ],
  injectApi: () =>
    Promise.all([
      import('store/api/graphApi'),
      import('store/api/ragApi'),
      import('store/api/aiModelsApi'),
    ]),
};
