import { Button, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';

/** One rail group's contribution to the bar — a dirty-field count or an error count. */
export interface ModuleSaveBarGroupCount {
  /** The `GroupNode.key` this count belongs to. */
  key: string;
  /** Translated group label, ready to render. */
  label: string;
  count: number;
}

export interface ModuleSaveBarErrorGroup extends ModuleSaveBarGroupCount {
  /** Navigates the rail to this group. */
  onSelect: () => void;
}

export interface ModuleSaveBarProps {
  /** Total unsaved, visible fields across every group — what a per-card form could never report. */
  dirtyCount: number;
  /**
   * Non-zero groups only, in rail order. Empty when there is no rail to
   * navigate — a per-group chip would just restate `dirtyCount`.
   */
  perGroup: ModuleSaveBarGroupCount[];
  /**
   * Total invalid, visible fields — independent of `errors` below, so the
   * aggregate "N fields need attention" message still renders even when
   * there's no rail to break it down by group.
   */
  errorCount: number;
  /**
   * Non-zero groups only, in rail order, each wired to jump the rail there.
   * Empty when there is no rail — a "Go to <group>" button would have
   * nowhere useful to navigate, since the field is already the only thing
   * on screen.
   */
  errors: ModuleSaveBarErrorGroup[];
  saving: boolean;
  onDiscard: () => void;
  onSave: () => void;
}

/**
 * One save bar for the whole module config form, pinned to the bottom of the
 * config surface (`.module-save-bar`, sticky). It is what makes the rail's
 * per-group panels behave like one form: an edit made in a group the operator
 * has since navigated away from still counts here, and a validation error in
 * an off-screen group gets a button that jumps the rail there — the inline
 * message under the field is otherwise invisible until you scroll back.
 */
const ModuleSaveBar: React.FC<ModuleSaveBarProps> = ({
  dirtyCount,
  perGroup,
  errorCount,
  errors,
  saving,
  onDiscard,
  onSave
}) => {
  const { t } = useTranslation();

  return (
    <div className="module-save-bar d-flex flex-wrap align-items-center justify-content-between gap-2 px-3 py-2 mt-3">
      <div className="d-flex flex-wrap align-items-center gap-2 fs-10">
        {dirtyCount > 0 && (
          <>
            <span className="fw-semibold">
              {t('adminModules.detail.saveBar.changes', { count: dirtyCount })}
            </span>
            {perGroup.map(group => (
              <span key={group.key} className="text-muted">
                {t('adminModules.detail.saveBar.perGroup', {
                  group: group.label,
                  count: group.count
                })}
              </span>
            ))}
          </>
        )}
        {errorCount > 0 && (
          <span className="text-danger fw-semibold ms-md-2">
            {t('adminModules.detail.saveBar.errors', { count: errorCount })}
          </span>
        )}
        {errors.map(group => (
          <Button
            key={group.key}
            type="button"
            variant="outline-danger"
            size="sm"
            onClick={group.onSelect}
          >
            {t('adminModules.detail.saveBar.goToError', { group: group.label })}
          </Button>
        ))}
      </div>
      <div className="d-flex gap-2 flex-shrink-0">
        {dirtyCount > 0 && (
          <Button
            type="button"
            variant="outline-secondary"
            size="sm"
            onClick={onDiscard}
            disabled={saving}
          >
            {t('adminModules.detail.configCard.discard')}
          </Button>
        )}
        <Button
          type="button"
          variant="primary"
          size="sm"
          onClick={onSave}
          disabled={saving || dirtyCount === 0}
        >
          {saving ? (
            <Spinner animation="border" size="sm" />
          ) : (
            t('adminModules.detail.configCard.saveChanges')
          )}
        </Button>
      </div>
    </div>
  );
};

export default ModuleSaveBar;
