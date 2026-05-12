import { Card } from 'react-bootstrap';
import { OrkestraCardHeader } from 'components/common';
import SubtleBadge from 'components/common/SubtleBadge';
import type { ModuleConfig } from 'store/api/moduleApi';

interface ModuleDependencyCardProps {
  module: ModuleConfig;
  allModules?: ModuleConfig[];
}

const ModuleDependencyCard: React.FC<ModuleDependencyCardProps> = ({
  module: mod,
  allModules
}) => {
  const hasDeps = mod.dependsOn && mod.dependsOn.length > 0;
  const hasProvided = mod.providedServices && mod.providedServices.length > 0;
  const hasRequired = mod.requiredServices && mod.requiredServices.length > 0;
  const hasOptional = mod.optionalServices && mod.optionalServices.length > 0;

  if (!hasDeps && !hasProvided && !hasRequired && !hasOptional) {
    return null;
  }

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title="Dependencies & Services" light={false} />
      <Card.Body className="py-3">
        {hasDeps && (
          <div className="mb-3">
            <div className="fw-semibold fs-10 text-600 mb-2">Depends On</div>
            {mod.dependsOn!.map(dep => {
              const depMod = allModules?.find(m => m.moduleName === dep);
              const depStatus = depMod?.status || 'unknown';
              const color =
                depStatus === 'running'
                  ? 'success'
                  : depStatus === 'disabled'
                    ? 'secondary'
                    : 'danger';
              return (
                <div key={dep} className="d-flex align-items-center gap-2 mb-1">
                  <span
                    className={`rounded-circle bg-${color}`}
                    style={{ width: 8, height: 8, display: 'inline-block' }}
                  />
                  <span className="fs-10">{dep}</span>
                  <SubtleBadge bg={color} pill className="fs-11">
                    {depStatus}
                  </SubtleBadge>
                </div>
              );
            })}
          </div>
        )}

        {hasProvided && (
          <div className="mb-3">
            <div className="fw-semibold fs-10 text-600 mb-2">
              Provided Services
            </div>
            <div className="d-flex flex-wrap gap-1">
              {mod.providedServices!.map(svc => (
                <SubtleBadge key={svc} bg="info" pill className="fs-11">
                  {svc}
                </SubtleBadge>
              ))}
            </div>
          </div>
        )}

        {hasRequired && (
          <div className="mb-3">
            <div className="fw-semibold fs-10 text-600 mb-2">
              Required Services
            </div>
            <div className="d-flex flex-wrap gap-1">
              {mod.requiredServices!.map(svc => (
                <SubtleBadge key={svc} bg="warning" pill className="fs-11">
                  {svc}
                </SubtleBadge>
              ))}
            </div>
          </div>
        )}

        {hasOptional && (
          <div>
            <div className="fw-semibold fs-10 text-600 mb-2">
              Optional Services
            </div>
            <div className="d-flex flex-wrap gap-1">
              {mod.optionalServices!.map(svc => (
                <SubtleBadge key={svc} bg="secondary" pill className="fs-11">
                  {svc}
                </SubtleBadge>
              ))}
            </div>
          </div>
        )}
      </Card.Body>
    </Card>
  );
};

export default ModuleDependencyCard;
