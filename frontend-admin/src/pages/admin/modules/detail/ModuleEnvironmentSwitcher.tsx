import { useState } from 'react';
import { Button, ButtonGroup, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faCheck } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import { useSetActiveEnvironmentMutation } from 'store/api/moduleApi';

interface ModuleEnvironmentSwitcherProps {
  moduleName: string;
  activeEnvironment: string;
  availableEnvironments: string[];
  selectedEnvironment: string;
  onSelect: (env: string) => void;
}

/**
 * Two different questions used to share one visual answer here, and that was
 * the defect: solid-primary meant "the profile you are looking at", while the
 * profile actually serving traffic was marked only by a small badge that
 * *moved* when you switched. Selecting sandbox gave it exactly the treatment
 * production wore a second earlier, so an operator could paste production
 * credentials into a sandbox profile, read "saved successfully", and lose an
 * hour to it.
 *
 * They are now separated. The button group answers *viewing* only. The active
 * profile carries a `LIVE` badge that never moves with selection, and the
 * caller renders a persistent warning strip whenever the two disagree (see
 * `detail/index.tsx`) — a strip, not a badge, because the header carrying the
 * badge scrolls out of view on a long config panel.
 *
 * Promotion is now confirmed. It used to fire immediately while merely
 * *switching what you view* raised a modal when the form was dirty — friction
 * inverted relative to consequence.
 */
const ModuleEnvironmentSwitcher: React.FC<ModuleEnvironmentSwitcherProps> = ({
  moduleName,
  activeEnvironment,
  availableEnvironments,
  selectedEnvironment,
  onSelect
}) => {
  const { t } = useTranslation();
  const [setActive, { isLoading }] = useSetActiveEnvironmentMutation();
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);

  const handleSetActive = async () => {
    if (selectedEnvironment === activeEnvironment) return;
    setError(null);
    try {
      await setActive({
        name: moduleName,
        environment: selectedEnvironment
      }).unwrap();
      setConfirming(false);
    } catch (err: unknown) {
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String(
              (err as { data: { detail?: string } }).data?.detail ||
                t('adminModules.detail.switchFailed')
            )
          : t('adminModules.detail.switchFailed');
      setError(message);
      setConfirming(false);
    }
  };

  return (
    <>
      <div className="d-flex align-items-center gap-3 mb-3 flex-wrap">
        <span className="fs-10 fw-semibold text-600">
          {t('adminModules.detail.env.viewingLabel')}
        </span>
        <ButtonGroup size="sm">
          {availableEnvironments.map(env => (
            <Button
              key={env}
              variant={
                selectedEnvironment === env ? 'primary' : 'outline-primary'
              }
              aria-pressed={selectedEnvironment === env}
              onClick={() => onSelect(env)}
              className="text-capitalize"
            >
              {env}
            </Button>
          ))}
        </ButtonGroup>

        {/* Anchored to the active environment's name, not to the selection —
            so it reads as a fact about the module rather than as feedback on
            what you just clicked. */}
        <span className="d-flex align-items-center gap-1 fs-10 text-600">
          <SubtleBadge bg="success" pill className="fs-11">
            {t('adminModules.detail.env.liveBadge')}
          </SubtleBadge>
          <span className="text-capitalize">{activeEnvironment}</span>
        </span>

        {selectedEnvironment !== activeEnvironment && (
          <Button
            variant="outline-success"
            size="sm"
            onClick={() => setConfirming(true)}
            disabled={isLoading}
          >
            {isLoading ? (
              <Spinner animation="border" size="sm" />
            ) : (
              <>
                <FontAwesomeIcon icon={faCheck} className="me-1" />
                {t('adminModules.detail.setAsActive')}
              </>
            )}
          </Button>
        )}

        {error && <span className="text-danger fs-11">{error}</span>}
      </div>

      <Modal show={confirming} centered onHide={() => setConfirming(false)}>
        <Modal.Header closeButton>
          <Modal.Title>
            {t('adminModules.detail.env.promoteTitle', {
              environment: selectedEnvironment
            })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body className="fs-10">
          {t('adminModules.detail.env.promoteBody', {
            environment: selectedEnvironment,
            current: activeEnvironment
          })}
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setConfirming(false)}
          >
            {t('adminModules.detail.env.cancel')}
          </Button>
          <Button
            variant="success"
            size="sm"
            onClick={handleSetActive}
            disabled={isLoading}
          >
            {t('adminModules.detail.env.promoteConfirm')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default ModuleEnvironmentSwitcher;
