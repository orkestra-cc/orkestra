import { useState, FormEvent } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import {
  useWebAuthnRegisterBeginMutation,
  useWebAuthnRegisterFinishMutation,
} from 'store/api/mfaApi';
import {
  browserSupportsWebAuthn,
  decodeCreationOptions,
  encodeAttestation,
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
      setError('This browser does not support passkeys. Try Chrome, Safari, or Firefox over HTTPS.');
      return;
    }
    const trimmed = name.trim() || 'Passkey';
    setBusy(true);
    try {
      const beginRes = await begin().unwrap();
      const opts = decodeCreationOptions(beginRes.publicKey);
      const cred = (await navigator.credentials.create({ publicKey: opts })) as PublicKeyCredential | null;
      if (!cred) {
        setError('Enrollment was cancelled. Try again when you are ready.');
        setBusy(false);
        return;
      }
      await finish({
        challengeId: beginRes.challengeId,
        name: trimmed,
        attestationResponse: encodeAttestation(cred),
      }).unwrap();
      reset();
      onHide();
    } catch (err: unknown) {
      const anyErr = err as { name?: string; message?: string; status?: number; data?: { detail?: string } };
      // DOMException from navigator.credentials.create — typically the
      // user cancelled the prompt or the authenticator timed out.
      if (anyErr?.name === 'NotAllowedError') {
        setError('The enrollment was cancelled or timed out. Try again.');
      } else if (anyErr?.name === 'InvalidStateError') {
        setError('This authenticator is already registered on your account.');
      } else if (anyErr?.status === 401) {
        setError('Could not verify the attestation. Please try again.');
      } else {
        setError(anyErr?.data?.detail ?? anyErr?.message ?? 'Could not register the passkey.');
      }
      setBusy(false);
    }
  };

  return (
    <Modal show={show} onHide={handleClose} backdrop="static" centered>
      <Modal.Header closeButton={!busy}>
        <Modal.Title>Add a passkey</Modal.Title>
      </Modal.Header>
      <Form onSubmit={handleSubmit} noValidate>
        <Modal.Body>
          <p className="fs-10 mb-3">
            A passkey replaces the authenticator-app code with a tap or biometric prompt.
            You can use a built-in fingerprint reader, Face ID, or a hardware security key.
          </p>

          {error && (
            <Alert variant="danger" className="mb-3" onClose={() => setError(null)} dismissible>
              {error}
            </Alert>
          )}

          <Form.Group className="mb-2">
            <Form.Label>Name</Form.Label>
            <Form.Control
              type="text"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Yubikey 5C, iPhone Touch ID"
              maxLength={60}
            />
            <Form.Text muted>
              A label so you can recognise this passkey later in the settings list.
            </Form.Text>
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="outline-secondary" onClick={handleClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? 'Waiting for authenticator…' : 'Register passkey'}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default WebAuthnEnrollDialog;
