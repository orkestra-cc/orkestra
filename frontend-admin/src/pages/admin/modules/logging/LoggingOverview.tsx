import { Col, Row } from 'react-bootstrap';
import {
  faBug,
  faLayerGroup,
  faServer,
  faSliders
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import StatCard from 'components/common/StatCard';
import type { LogLevelsView } from 'types/observability';

interface LoggingOverviewProps {
  snapshot: LogLevelsView;
}

const LoggingOverview = ({ snapshot }: LoggingOverviewProps) => {
  const { t } = useTranslation();
  const overrideCount = snapshot.modules.filter(
    module => module.hasOverride
  ).length;
  const diagnosticCount = snapshot.diagnostics.length;

  return (
    <Row className="g-3 mb-3">
      <Col md={6} xl={3}>
        <StatCard
          title={t('adminObservability.loggingWorkspace.overview.global')}
          value={t(
            `adminObservability.loggingWorkspace.levelNames.${snapshot.global}`
          )}
          icon={faSliders}
          color="primary"
          subtitle={t(
            `adminObservability.loggingWorkspace.levelDescriptions.${snapshot.global}`
          )}
        />
      </Col>
      <Col md={6} xl={3}>
        <StatCard
          title={t('adminObservability.loggingWorkspace.overview.overrides')}
          value={overrideCount}
          icon={faLayerGroup}
          color="info"
          subtitle={t(
            'adminObservability.loggingWorkspace.overview.overridesSubtitle',
            { count: snapshot.modules.length }
          )}
        />
      </Col>
      <Col md={6} xl={3}>
        <StatCard
          title={t('adminObservability.loggingWorkspace.overview.diagnostics')}
          value={diagnosticCount}
          icon={faBug}
          color={diagnosticCount > 0 ? 'warning' : 'secondary'}
          accent={diagnosticCount > 0 ? 'warning' : undefined}
          subtitle={t(
            diagnosticCount > 0
              ? 'adminObservability.loggingWorkspace.overview.diagnosticsActive'
              : 'adminObservability.loggingWorkspace.overview.diagnosticsIdle'
          )}
        />
      </Col>
      <Col md={6} xl={3}>
        <StatCard
          title={t('adminObservability.loggingWorkspace.overview.provider')}
          value={t(
            snapshot.logProvider.available
              ? 'adminObservability.loggingWorkspace.overview.available'
              : 'adminObservability.loggingWorkspace.overview.unavailable'
          )}
          icon={faServer}
          color={snapshot.logProvider.available ? 'success' : 'secondary'}
          subtitle={t(
            snapshot.logProvider.available
              ? 'adminObservability.loggingWorkspace.overview.providerReady'
              : 'adminObservability.loggingWorkspace.overview.providerOptional'
          )}
        />
      </Col>
    </Row>
  );
};

export default LoggingOverview;
