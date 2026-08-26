import { useState } from 'react';
import { Button, Card, Form, InputGroup } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faPlus,
  faRotateLeft,
  faTrash
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import type { ConfigField } from 'store/api/moduleApi';
import { translateConfigField } from 'helpers/configLabel';
import { MAX_LABEL_LENGTH, isValidSlug, mintSlug } from './mintSlug';

export interface RecordListFieldProps {
  field: ConfigField;
  /** Owning module — selects the i18n namespace the list's own label resolves against. */
  moduleName: string;
  /** Element slugs in stored order, including any added but not yet saved. */
  roster: string[];
  /** Slug → current display label, for the card headers. */
  labels: Record<string, string>;
  /** Slugs marked for removal at the next save. Still rendered, muted. */
  staged: string[];
  onCreate: (slug: string, label: string) => void;
  onStageRemove: (slug: string) => void;
  onUndoRemove: (slug: string) => void;
  /**
   * Renders one element's body. The container owns the card chrome and the
   * membership intents; the fields inside are ordinary concrete fields and go
   * through the existing leaf renderer, which is why this is a callback
   * rather than something this component builds itself.
   */
  renderElement: (slug: string) => React.ReactNode;
}

/**
 * The operator-facing surface of a `recordList`: one card per element, an add
 * flow that previews the slug it is about to mint, and a **staged** delete.
 *
 * Removal is staged rather than immediate because it is irreversible on the
 * backend — the element's keys, including its encrypted secrets, are dropped
 * for good. A card that vanishes the moment a trash icon is clicked reads
 * like it was hidden; keeping it on screen, muted, with an Undo, tells the
 * truth: nothing has happened yet, and the destructive step is Save.
 *
 * The slug is minted once, at creation, and never moves again — renaming an
 * element changes only its label. That is what lets an element's stored keys
 * survive a rename, and it is why the preview matters: the operator is
 * choosing a permanent key, not just a caption.
 */
export const RecordListField: React.FC<RecordListFieldProps> = ({
  field,
  moduleName,
  roster,
  labels,
  staged,
  onCreate,
  onStageRemove,
  onUndoRemove,
  renderElement
}) => {
  const { t } = useTranslation();
  const [adding, setAdding] = useState(false);
  const [draftName, setDraftName] = useState('');

  const label = translateConfigField(t, moduleName, field, 'label');
  const desc = translateConfigField(t, moduleName, field, 'desc');

  const draftSlug = mintSlug(draftName);
  const taken = roster.includes(draftSlug);
  const canConfirm =
    isValidSlug(draftSlug) &&
    !taken &&
    draftName.trim().length <= MAX_LABEL_LENGTH;

  const closeAdd = () => {
    setAdding(false);
    setDraftName('');
  };

  const confirmAdd = () => {
    if (!canConfirm) return;
    onCreate(draftSlug, draftName.trim());
    closeAdd();
  };

  return (
    <div className="mb-3">
      <div className="d-flex align-items-center justify-content-between mb-2">
        <Form.Label className="mb-0">{label}</Form.Label>
        <Button
          variant="falcon-default"
          size="sm"
          onClick={() => setAdding(true)}
          disabled={adding}
        >
          <FontAwesomeIcon icon={faPlus} className="me-1" />
          {t('adminModules.recordList.add')}
        </Button>
      </div>
      {desc && (
        <Form.Text className="text-muted d-block mb-2">{desc}</Form.Text>
      )}

      {roster.length === 0 && !adding && (
        <p className="text-muted fs-10 mb-0">
          {t('adminModules.recordList.empty')}
        </p>
      )}

      {roster.map(slug => {
        const isStaged = staged.includes(slug);
        return (
          <Card
            key={slug}
            className={`mb-2 ${isStaged ? 'opacity-50 border-danger' : ''}`}
          >
            <Card.Header className="d-flex align-items-center justify-content-between py-2">
              <div>
                <span className="fw-semibold fs-10">
                  {labels[slug] || slug}
                </span>
                <code className="ms-2 fs-11 text-muted">{slug}</code>
                {isStaged && (
                  <span className="badge badge-subtle-danger ms-2 fs-11">
                    {t('adminModules.recordList.stagedNotice')}
                  </span>
                )}
              </div>
              {isStaged ? (
                <Button
                  variant="link"
                  size="sm"
                  className="text-decoration-none"
                  onClick={() => onUndoRemove(slug)}
                >
                  <FontAwesomeIcon icon={faRotateLeft} className="me-1" />
                  {t('adminModules.recordList.undo')}
                </Button>
              ) : (
                <Button
                  variant="link"
                  size="sm"
                  className="text-danger text-decoration-none"
                  onClick={() => onStageRemove(slug)}
                >
                  <FontAwesomeIcon icon={faTrash} className="me-1" />
                  {t('adminModules.recordList.remove')}
                </Button>
              )}
            </Card.Header>
            {!isStaged && <Card.Body>{renderElement(slug)}</Card.Body>}
          </Card>
        );
      })}

      {adding && (
        <Card className="mb-2">
          <Card.Body>
            <Form.Group>
              <Form.Label htmlFor={`rl-add-${field.key}`}>
                {t('adminModules.recordList.nameLabel')}
              </Form.Label>
              <InputGroup size="sm">
                <Form.Control
                  id={`rl-add-${field.key}`}
                  value={draftName}
                  autoFocus
                  maxLength={MAX_LABEL_LENGTH}
                  onChange={e => setDraftName(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      confirmAdd();
                    }
                  }}
                />
                <Button
                  variant="falcon-primary"
                  disabled={!canConfirm}
                  onClick={confirmAdd}
                >
                  {t('adminModules.recordList.confirm')}
                </Button>
                <Button variant="falcon-default" onClick={closeAdd}>
                  {t('adminModules.recordList.cancel')}
                </Button>
              </InputGroup>
              <Form.Text className="text-muted">
                {draftSlug ? (
                  <>
                    {t('adminModules.recordList.slugPreview')}{' '}
                    <code>{draftSlug}</code>
                    {taken && (
                      <span className="text-danger ms-2">
                        {t('adminModules.recordList.slugTaken')}
                      </span>
                    )}
                  </>
                ) : (
                  t('adminModules.recordList.slugEmpty')
                )}
              </Form.Text>
            </Form.Group>
          </Card.Body>
        </Card>
      )}
    </div>
  );
};

export default RecordListField;
