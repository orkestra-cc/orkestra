import { useEffect, useState, type FormEvent } from 'react';
import { Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { LegalHold } from 'store/api/complianceApi';
import { formatDateTime } from './complianceFormat';

interface Props {
  hold: LegalHold | null;
  busy: boolean;
  onClose: () => void;
  onAdd: (text: string) => Promise<boolean>;
}

// LegalHoldNotesModal lists a hold's notes and appends a new one (1-1000
// characters). The note field clears only when the backend accepted it.
const LegalHoldNotesModal = ({ hold, busy, onClose, onAdd }: Props) => {
  const { t } = useTranslation();
  const [text, setText] = useState('');
  useEffect(() => setText(''), [hold?.uuid]);
  const trimmed = text.trim();
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!trimmed || trimmed.length > 1000) return;
    if (await onAdd(trimmed)) setText('');
  };
  const notes = hold?.notes ?? [];
  // An expired or released hold is history: no new notes.
  const readOnly = !!hold && (hold.expired || !hold.active);
  return (
    <Modal show={!!hold} onHide={onClose} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h5">
          {t('adminCompliance.holds.notes.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {notes.length === 0 ? (
          <p className="fs-10 text-muted">
            {t('adminCompliance.holds.notes.empty')}
          </p>
        ) : (
          <ul className="list-unstyled fs-10">
            {notes.map(n => (
              <li key={n.id} className="mb-2">
                <div className="text-600">
                  {formatDateTime(n.at)} ·{' '}
                  <span className="font-monospace">{n.by}</span>
                </div>
                <div>{n.text}</div>
              </li>
            ))}
          </ul>
        )}
        {readOnly ? (
          <p className="fs-10 text-muted mb-0">
            {t('adminCompliance.holds.notes.readOnly')}
          </p>
        ) : (
          <Form onSubmit={submit}>
            <Form.Group controlId="hold-note-text">
              <Form.Label>{t('adminCompliance.holds.notes.label')}</Form.Label>
              <Form.Control
                as="textarea"
                rows={2}
                maxLength={1000}
                value={text}
                onChange={e => setText(e.target.value)}
              />
            </Form.Group>
            <Button
              className="mt-2"
              type="submit"
              size="sm"
              variant="orkestra-primary"
              disabled={busy || !trimmed}
            >
              {t('adminCompliance.holds.notes.add')}
            </Button>
          </Form>
        )}
      </Modal.Body>
    </Modal>
  );
};

export default LegalHoldNotesModal;
