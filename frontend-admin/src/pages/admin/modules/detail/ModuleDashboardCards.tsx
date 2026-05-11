import { Card, Col, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faHeartPulse,
  faGear,
  faSitemap,
  faClock,
} from '@fortawesome/free-solid-svg-icons';
import SubtleBadge from 'components/common/SubtleBadge';
import type { ModuleConfig, ModuleHealthStatus } from 'store/api/moduleApi';
import { configCompleteness } from '../utils';

interface ModuleDashboardCardsProps {
  module: ModuleConfig;
  health?: ModuleHealthStatus;
  allModules?: ModuleConfig[];
}

const formatRelativeTime = (dateStr: string): string => {
  if (!dateStr) return '\u2014';
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
};

const ModuleDashboardCards: React.FC<ModuleDashboardCardsProps> = ({
  module: mod,
  health,
  allModules,
}) => {
  const healthStatus = health?.status || (mod.enabled ? 'healthy' : 'disabled');
  const healthColor = {
    healthy: 'success',
    unhealthy: 'danger',
    disabled: 'secondary',
    failed: 'danger',
  }[healthStatus] || 'secondary';

  const { filled, total } = configCompleteness(
    mod.configSchema,
    mod.configValues,
    mod.secretStatus
  );

  const depCount = mod.dependsOn?.length || 0;
  const depsHealthy = mod.dependsOn?.filter((dep) => {
    const depMod = allModules?.find((m) => m.moduleName === dep);
    return depMod && depMod.status === 'running';
  }).length || 0;

  return (
    <Row className="g-3 mb-3">
      <Col sm={6} lg={3}>
        <Card className="h-100">
          <Card.Body className="py-3 px-4">
            <div className="d-flex align-items-center mb-2">
              <FontAwesomeIcon icon={faHeartPulse} className="text-400 me-2" />
              <span className="fs-10 text-600 fw-semibold">Health</span>
            </div>
            <SubtleBadge bg={healthColor as 'success' | 'danger' | 'secondary'} pill>
              {healthStatus}
            </SubtleBadge>
            {health?.error && (
              <div className="text-danger fs-11 mt-1" title={health.error}>
                {health.error.length > 40 ? health.error.slice(0, 40) + '...' : health.error}
              </div>
            )}
          </Card.Body>
        </Card>
      </Col>

      <Col sm={6} lg={3}>
        <Card className="h-100">
          <Card.Body className="py-3 px-4">
            <div className="d-flex align-items-center mb-2">
              <FontAwesomeIcon icon={faGear} className="text-400 me-2" />
              <span className="fs-10 text-600 fw-semibold">Configuration</span>
            </div>
            <div className="fs-8 fw-bold text-900">
              {total > 0 ? `${filled}/${total}` : '\u2014'}
            </div>
            <div className="text-muted fs-11">
              {total > 0 ? 'required fields set' : 'no required fields'}
            </div>
            {total > 0 && (
              <div className="progress mt-2" style={{ height: '4px' }}>
                <div
                  className={`progress-bar bg-${filled === total ? 'success' : 'warning'}`}
                  style={{ width: `${total > 0 ? (filled / total) * 100 : 0}%` }}
                />
              </div>
            )}
          </Card.Body>
        </Card>
      </Col>

      <Col sm={6} lg={3}>
        <Card className="h-100">
          <Card.Body className="py-3 px-4">
            <div className="d-flex align-items-center mb-2">
              <FontAwesomeIcon icon={faSitemap} className="text-400 me-2" />
              <span className="fs-10 text-600 fw-semibold">Dependencies</span>
            </div>
            <div className="fs-8 fw-bold text-900">
              {depCount > 0 ? `${depsHealthy}/${depCount}` : '\u2014'}
            </div>
            <div className="text-muted fs-11">
              {depCount > 0 ? 'dependencies running' : 'no dependencies'}
            </div>
          </Card.Body>
        </Card>
      </Col>

      <Col sm={6} lg={3}>
        <Card className="h-100">
          <Card.Body className="py-3 px-4">
            <div className="d-flex align-items-center mb-2">
              <FontAwesomeIcon icon={faClock} className="text-400 me-2" />
              <span className="fs-10 text-600 fw-semibold">Last Modified</span>
            </div>
            <div className="fs-8 fw-bold text-900">
              {formatRelativeTime(mod.updatedAt)}
            </div>
            <div className="text-muted fs-11">
              {mod.updatedAt
                ? new Date(mod.updatedAt).toLocaleDateString('en-GB', {
                    day: '2-digit',
                    month: 'short',
                    year: 'numeric',
                  })
                : ''}
            </div>
          </Card.Body>
        </Card>
      </Col>
    </Row>
  );
};

export default ModuleDashboardCards;
