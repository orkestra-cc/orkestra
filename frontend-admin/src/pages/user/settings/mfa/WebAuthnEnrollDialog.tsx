import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import {
  useWebAuthnRegisterBeginMutation,
  useWebAuthnRegisterFinishMutation
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeCreationOptions,
  encodeAttestation
} from 'store/api/webauthnCodec';

interface Props {
  show: boolean;
  onHide: () => void;
}

/**
 * Single-step WebAuthn enrollment dialog. The user picks a label, the
 * dialog runs the W3C ceremony (begin → navigator.credentials.create →
 * finish), and the credential lands in the backend. Three failure modes
 * to consider:
 *   - browser not supported (rare on modern Chrome/Safari/Firefox);
 *   - user cancels the platform prompt — surfaces as DOMException;
 *   - server rejects the attestation — surfaces with detail in `data`.
 */
const WebAuthnEnrollDialog = ({ show, onHide }: Props) => {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [begin] = useWebAuthnRegisterBeginMutation();
  const [finish] = useWebAuthnRegisterFinishMutation();

  const reset = () => {
    setName('');
    setError(null);
    setBusy(false);
  };

  const handleClose = () => {
    if (busy) return;
    reset();
    onHide();
  };

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (!browserSupportsWebAuthn()) {
      setError(t('userMfa.webauthn.enroll.errors.unsupported'));
      return;
    }
    const trimmed = name.trim() || 'Passkey';
    setBusy(true);
    try {
      const beginRes = await begin().unwrap();
      const opts = decodeCreationOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.create({
        publicKey: opts
      })) as PublicKeyCredential | null;
      if (!cred) {
        setError(t('userMfa.webauthn.enroll.errors.cancelled'));
        setBusy(false);
        return;
      }
      await finish({
        challengeId: beginRes.challengeId,
        name: trimmed,
        attestationResponse: encodeAttestation(cred)
      }).unwrap();
      reset();
      onHide();
    } catch (err: unknown) {
      const anyErr = err as {
        name?: string;
        message?: string;
        status?: number;
        data?: { detail?: string };
      };
      // DOMException from navigator.credentials.create — typically the
      // user cancelled the prompt or the authenticator timed out.
      if (anyErr?.name === 'NotAllowedError') {
        setError(t('userMfa.webauthn.enroll.errors.cancelledOrTimedOut'));
      } else if (anyErr?.name === 'InvalidStateError') {
        setError(t('userMfa.webauthn.enroll.errors.alreadyRegistered'));
      } else if (anyErr?.status === 401) {
        setError(t('userMfa.webauthn.enroll.errors.attestationFailed'));
      } else {
        setError(
          anyErr?.data?.detail ??
            anyErr?.message ??
            t('userMfa.webauthn.enroll.errors.generic')
        );
      }
      setBusy(false);
    }
  };

  return (
    <Modal show={show} onHide={handleClose} backdrop="static" centered>
      <Modal.Header closeButton={!busy}>
        <Modal.Title>{t('userMfa.webauthn.enroll.title')}</Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <p className="fs-10 mb-3">{t('userMfa.webauthn.enroll.intro')}</p>

          {error && (
            <Alert
              variant="danger"
              className="mb-3"
              onClose={() => setError(null)}
              dismissible
            >
              {error}
            </Alert>
          )}

          <Form.Group className="mb-2">
            <Form.Label>{t('userMfa.webauthn.enroll.nameLabel')}</Form.Label>
            <Form.Control
              type="text"
              autoFocus
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder={t('userMfa.webauthn.enroll.namePlaceholder')}
              maxLength={60}
            />
            <Form.Text muted>{t('userMfa.webauthn.enroll.nameHint')}</Form.Text>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="outline-secondary"
            onClick={handleClose}
            disabled={busy}
          >
            {t('userMfa.webauthn.enroll.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy
              ? t('userMfa.webauthn.enroll.registering')
              : t('userMfa.webauthn.enroll.register')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default WebAuthnEnrollDialog;
