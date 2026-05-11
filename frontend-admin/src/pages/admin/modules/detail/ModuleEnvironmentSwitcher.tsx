import { useState } from 'react';
import { Button, ButtonGroup, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faCheck } from '@fortawesome/free-solid-svg-icons';
import SubtleBadge from 'components/common/SubtleBadge';
import { useSetActiveEnvironmentMutation } from 'store/api/moduleApi';

interface ModuleEnvironmentSwitcherProps {
  moduleName: string;
  activeEnvironment: string;
  availableEnvironments: string[];
  selectedEnvironment: string;
  onSelect: (env: string) => void;
}

const ModuleEnvironmentSwitcher: React.FC<ModuleEnvironmentSwitcherProps> = ({
  moduleName,
  activeEnvironment,
  availableEnvironments,
  selectedEnvironment,
  onSelect,
}) => {
  const [setActive, { isLoading }] = useSetActiveEnvironmentMutation();
  const [error, setError] = useState<string | null>(null);

  const handleSetActive = async () => {
    if (selectedEnvironment === activeEnvironment) return;
    setError(null);
    try {
      await setActive({ name: moduleName, environment: selectedEnvironment }).unwrap();
    } catch (err: unknown) {
      const message =
        err && typeof err === 'object' && 'data' in err
          ? String((err as { data: { detail?: string } }).data?.detail || 'Failed to switch environment')
          : 'Failed to switch environment';
      setError(message);
    }
  };

  return (
    <div className="d-flex align-items-center gap-3 mb-3 flex-wrap">
      <span className="fs-10 fw-semibold text-600">Environment:</span>
      <ButtonGroup size="sm">
        {availableEnvironments.map((env) => (
          <Button
            key={env}
            variant={selectedEnvironment === env ? 'primary' : 'outline-primary'}
            onClick={() => onSelect(env)}
            className="text-capitalize"
          >
            {env}
            {env === activeEnvironment && (
              <SubtleBadge bg="success" pill className="ms-1 fs-11">
                active
              </SubtleBadge>
            )}
          </Button>
        ))}
      </ButtonGroup>

      {selectedEnvironment !== activeEnvironment && (
        <Button
          variant="outline-success"
          size="sm"
          onClick={handleSetActive}
          disabled={isLoading}
        >
          {isLoading ? (
            <Spinner animation="border" size="sm" />
          ) : (
            <>
              <FontAwesomeIcon icon={faCheck} className="me-1" />
              Set as Active
            </>
          )}
        </Button>
      )}

      {error && <span className="text-danger fs-11">{error}</span>}
    </div>
  );
};

export default ModuleEnvironmentSwitcher;
