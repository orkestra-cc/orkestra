import { useEffect, useState } from 'react';
import { Alert, Button, Col, Form, Row, Spinner } from 'react-bootstrap';
import { faBug, faClock } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import SectionCard from 'components/common/SectionCard';
import SubtleBadge, { type BadgeColor } from 'components/common/SubtleBadge';
import { formatDateTime } from 'helpers/dateFormat';
import {
  useStartDiagnosticMutation,
  useStopDiagnosticMutation
} from 'store/api/observabilityApi';
import {
  LOG_LEVELS,
  type DiagnosticDurationMinutes,
  type LogLevel,
  type LogLevelsView
} from 'types/observability';

interface DiagnosticsPanelProps {
  snapshot: LogLevelsView;
}

type DurationSelection = `${DiagnosticDurationMinutes}` | 'none';

const levelVariant: Record<LogLevel, BadgeColor> = {
  debug: 'secondary',
  info: 'primary',
  warn: 'warning',
  error: 'danger'
};

const DiagnosticsPanel = ({ snapshot }: DiagnosticsPanelProps) => {
  const { t } = useTranslation();
  const [moduleName, setModuleName] = useState(snapshot.modules[0]?.name ?? '');
  const [level, setLevel] = useState<LogLevel>('debug');
  const [duration, setDuration] = useState<DurationSelection>('60');
  const [now, setNow] = useState(Date.now());
  const [stoppingModule, setStoppingModule] = useState<string | null>(null);
  const [actionError, setActionError] = useState(false);
  const [startDiagnostic, startStatus] = useStartDiagnosticMutation();
  const [stopDiagnostic] = useStopDiagnosticMutation();

  const hasExpiringDiagnostic = snapshot.diagnostics.some(
    diagnostic => diagnostic.expiresAt
  );

  // Countdown text is the one piece of this panel that changes without a user
  // event or a server response. A one-second timer is therefore necessary;
  // expiry authority still remains the server timestamps in the snapshot.
  useEffect(() => {
    if (!hasExpiringDiagnostic) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [hasExpiringDiagnostic]);

  const formatRemaining = (expiresAt: string): string => {
    const seconds = Math.max(
      0,
      Math.floor((new Date(expiresAt).getTime() - now) / 1000)
    );
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const remainingSeconds = seconds % 60;
    if (hours > 0) {
      return t(
        'adminObservability.loggingWorkspace.diagnostics.remainingHours',
        { hours, minutes, seconds: remainingSeconds }
      );
    }
    return t(
      'adminObservability.loggingWorkspace.diagnostics.remainingMinutes',
      { minutes, seconds: remainingSeconds }
    );
  };

  const handleStart = async () => {
    if (!moduleName) return;
    setActionError(false);
    try {
      await startDiagnostic({
        module: moduleName,
        level,
        durationMinutes:
          duration === 'none'
            ? undefined
            : (Number(duration) as DiagnosticDurationMinutes)
      }).unwrap();
    } catch {
      setActionError(true);
    }
  };

  const handleStop = async (targetModule: string) => {
    setStoppingModule(targetModule);
    setActionError(false);
    try {
      await stopDiagnostic({ module: targetModule }).unwrap();
    } catch {
      setActionError(true);
    } finally {
      setStoppingModule(null);
    }
  };

  return (
    <div className="d-flex flex-column gap-3">
      <SectionCard
        icon={faBug}
        title={t('adminObservability.loggingWorkspace.diagnostics.createTitle')}
      >
        <p className="text-muted fs-10">
          {t('adminObservability.loggingWorkspace.diagnostics.description')}
        </p>
        <Row className="g-3 align-items-end">
          <Col md={4}>
            <Form.Group controlId="logging-diagnostic-module">
              <Form.Label>
                {t(
                  'adminObservability.loggingWorkspace.diagnostics.moduleLabel'
                )}
              </Form.Label>
              <Form.Select
                value={moduleName}
                aria-label={t(
                  'adminObservability.loggingWorkspace.diagnostics.moduleAria'
                )}
                onChange={event => setModuleName(event.target.value)}
              >
                {snapshot.modules.map(module => (
                  <option key={module.name} value={module.name}>
                    {module.name}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          </Col>
          <Col md={3}>
            <Form.Group controlId="logging-diagnostic-level">
              <Form.Label>
                {t(
                  'adminObservability.loggingWorkspace.diagnostics.levelLabel'
                )}
              </Form.Label>
              <Form.Select
                value={level}
                aria-label={t(
                  'adminObservability.loggingWorkspace.diagnostics.levelAria'
                )}
                onChange={event => setLevel(event.target.value as LogLevel)}
              >
                {LOG_LEVELS.map(candidate => (
                  <option key={candidate} value={candidate}>
                    {t(
                      `adminObservability.loggingWorkspace.levelNames.${candidate}`
                    )}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          </Col>
          <Col md={3}>
            <Form.Group controlId="logging-diagnostic-duration">
              <Form.Label>
                {t(
                  'adminObservability.loggingWorkspace.diagnostics.durationLabel'
                )}
              </Form.Label>
              <Form.Select
                value={duration}
                aria-label={t(
                  'adminObservability.loggingWorkspace.diagnostics.durationAria'
                )}
                onChange={event =>
                  setDuration(event.target.value as DurationSelection)
                }
              >
                <option value="15">
                  {t(
                    'adminObservability.loggingWorkspace.diagnostics.duration15'
                  )}
                </option>
                <option value="60">
                  {t(
                    'adminObservability.loggingWorkspace.diagnostics.duration60'
                  )}
                </option>
                <option value="240">
                  {t(
                    'adminObservability.loggingWorkspace.diagnostics.duration240'
                  )}
                </option>
                <option value="none">
                  {t(
                    'adminObservability.loggingWorkspace.diagnostics.durationNone'
                  )}
                </option>
              </Form.Select>
            </Form.Group>
          </Col>
          <Col md={2}>
            <Button
              variant="falcon-primary"
              className="w-100"
              disabled={!moduleName || startStatus.isLoading}
              onClick={handleStart}
            >
              {startStatus.isLoading && (
                <Spinner animation="border" size="sm" className="me-2" />
              )}
              {t('adminObservability.loggingWorkspace.diagnostics.start')}
            </Button>
          </Col>
        </Row>

        <p className="text-muted fs-10 mt-3 mb-0">
          {t(`adminObservability.loggingWorkspace.levelDescriptions.${level}`)}
        </p>
        {level === 'debug' && (
          <Alert variant="warning" className="fs-10 py-2 mt-3 mb-0">
            {t('adminObservability.loggingWorkspace.diagnostics.debugWarning')}
          </Alert>
        )}
        {duration === 'none' && (
          <Alert variant="warning" className="fs-10 py-2 mt-3 mb-0">
            {t(
              'adminObservability.loggingWorkspace.diagnostics.noExpiryDraftWarning'
            )}
          </Alert>
        )}
      </SectionCard>

      {actionError && (
        <Alert variant="danger" className="fs-10 mb-0">
          {t('adminObservability.loggingWorkspace.diagnostics.actionFailed')}
        </Alert>
      )}

      <SectionCard
        icon={faClock}
        title={t('adminObservability.loggingWorkspace.diagnostics.activeTitle')}
      >
        {snapshot.diagnostics.length === 0 ? (
          <p className="text-muted fs-10 mb-0">
            {t('adminObservability.loggingWorkspace.diagnostics.empty')}
          </p>
        ) : (
          <div className="d-flex flex-column gap-3">
            {snapshot.diagnostics.map(diagnostic => (
              <div
                key={diagnostic.module}
                className="border rounded p-3 d-flex flex-column gap-2"
              >
                <div className="d-flex flex-wrap align-items-center justify-content-between gap-2">
                  <div className="d-flex flex-wrap align-items-center gap-2">
                    <code>{diagnostic.module}</code>
                    <SubtleBadge bg={levelVariant[diagnostic.level]} pill>
                      {t(
                        `adminObservability.loggingWorkspace.levelNames.${diagnostic.level}`
                      )}
                    </SubtleBadge>
                    {diagnostic.expiresAt && (
                      <span className="fs-10 fw-semibold text-700">
                        {formatRemaining(diagnostic.expiresAt)}
                      </span>
                    )}
                  </div>
                  <Button
                    variant="falcon-danger"
                    size="sm"
                    disabled={stoppingModule === diagnostic.module}
                    aria-label={t(
                      'adminObservability.loggingWorkspace.diagnostics.stopAria',
                      { module: diagnostic.module }
                    )}
                    onClick={() => handleStop(diagnostic.module)}
                  >
                    {stoppingModule === diagnostic.module && (
                      <Spinner animation="border" size="sm" className="me-2" />
                    )}
                    {t('adminObservability.loggingWorkspace.diagnostics.stop')}
                  </Button>
                </div>
                <span className="text-muted fs-10">
                  {t(
                    'adminObservability.loggingWorkspace.diagnostics.startedBy',
                    {
                      user: diagnostic.startedBy,
                      date: formatDateTime(diagnostic.startedAt)
                    }
                  )}
                </span>
                {!diagnostic.expiresAt && (
                  <Alert variant="warning" className="fs-10 py-2 mb-0">
                    {t(
                      'adminObservability.loggingWorkspace.diagnostics.noExpiryWarning'
                    )}
                  </Alert>
                )}
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  );
};

export default DiagnosticsPanel;
