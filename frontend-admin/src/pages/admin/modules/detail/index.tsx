import { lazy, Suspense, useState } from 'react';
import { Navigate, useParams } from 'react-router';
import { Col, Row, Spinner } from 'react-bootstrap';
import {
  useGetModuleQuery,
  useGetModulesQuery,
  useGetModulesHealthQuery,
} from 'store/api/moduleApi';
import ModuleDetailHeader from './ModuleDetailHeader';
import ModuleDashboardCards from './ModuleDashboardCards';
import ModuleEnvironmentSwitcher from './ModuleEnvironmentSwitcher';
import ModuleConfigSection from './ModuleConfigSection';
import ModuleDependencyCard from './ModuleDependencyCard';

const AIModelsConfigSection = lazy(() => import('./AIModelsConfigSection'));

const ModuleDetailPage: React.FC = () => {
  const { moduleName } = useParams<{ moduleName: string }>();
  const { data: mod, isLoading, error } = useGetModuleQuery(moduleName!, { skip: !moduleName });
  const { data: allModules } = useGetModulesQuery();
  const { data: healthData } = useGetModulesHealthQuery();

  const [selectedEnv, setSelectedEnv] = useState<string | null>(null);

  if (!moduleName) return <Navigate to="/admin/modules" replace />;
  if (isLoading) {
    return (
      <div className="text-center py-5">
        <Spinner animation="border" />
      </div>
    );
  }
  if (error || !mod) {
    return <Navigate to="/admin/modules" replace />;
  }

  const health = healthData?.modules.find((h) => h.moduleName === moduleName);
  const activeEnv = mod.activeEnvironment || 'production';
  const environments = mod.availableEnvironments?.length
    ? mod.availableEnvironments
    : ['production', 'sandbox'];
  const currentEnv = selectedEnv || activeEnv;

  return (
    <Row className="g-3">
      <Col xxl={12}>
        <ModuleDetailHeader module={mod} />

        <ModuleDashboardCards
          module={mod}
          health={health}
          allModules={allModules}
        />

        {environments.length > 1 && (
          <ModuleEnvironmentSwitcher
            moduleName={mod.moduleName}
            activeEnvironment={activeEnv}
            availableEnvironments={environments}
            selectedEnvironment={currentEnv}
            onSelect={setSelectedEnv}
          />
        )}

        {mod.moduleName === 'aimodels' ? (
          <Suspense fallback={<Spinner animation="border" size="sm" />}>
            <AIModelsConfigSection
              module={mod}
              selectedEnvironment={currentEnv}
            />
          </Suspense>
        ) : (
          <ModuleConfigSection
            module={mod}
            selectedEnvironment={currentEnv}
          />
        )}

        <ModuleDependencyCard module={mod} allModules={allModules} />
      </Col>
    </Row>
  );
};

export default ModuleDetailPage;
