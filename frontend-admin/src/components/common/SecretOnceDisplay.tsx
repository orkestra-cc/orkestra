import { useId, useState } from 'react';
import { Alert, Form } from 'react-bootstrap';
import { faCheck, faCopy } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import IconButton from 'components/common/IconButton';

interface SecretOnceDisplayProps {
  label: string;
  secret: string;
  secondaryLabel?: string;
  secondaryValue?: string;
  ack: boolean;
  onAckChange: (v: boolean) => void;
}

type CopyTarget = 'secondary' | 'secret';

// SecretOnceDisplay renders a value the backend will only ever return once
// (a freshly issued client secret, API key, ...): a warning banner, the
// value in a monospace block with a copy-to-clipboard affordance, and an
// acknowledgement checkbox the caller gates its own "close"/"done" action
// on. Modeled on the shown-once pattern in
// pages/user/security/BackupCodesDisplay.tsx (+ its MfaEnrollWizard caller).
//
// Deliberately NO download button: a secret saved to a file on disk is a
// worse leak surface than the clipboard, so unlike BackupCodesDisplay this
// component omits that affordance entirely.
//
// `ack` is fully controlled — the caller owns the acknowledgement state (so
// multi-step callers such as create/regenerate wizards can gate their own
// "Done" button on it); this component never tracks it internally.
const SecretOnceDisplay = ({
  label,
  secret,
  secondaryLabel,
  secondaryValue,
  ack,
  onAckChange
}: SecretOnceDisplayProps) => {
  const { t } = useTranslation();
  const [copiedField, setCopiedField] = useState<CopyTarget | null>(null);
  // The reference showcase mounts several live instances of this component
  // on one page — a fixed id would collide, cross-toggling every instance's
  // ack checkbox off the same label.
  const ackId = useId();

  const copy = async (field: CopyTarget, value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopiedField(field);
      window.setTimeout(() => {
        setCopiedField(current => (current === field ? null : current));
      }, 2000);
    } catch {
      // Clipboard API unavailable/denied — the value is still readable and
      // selectable directly from the monospace block below.
    }
  };

  return (
    <>
      <Alert variant="warning" className="mb-3">
        <strong>{t('common.secretOnce.warningPrefix')}</strong>{' '}
        {t('common.secretOnce.warningBody')}
      </Alert>

      {secondaryLabel && secondaryValue && (
        <div className="mb-3">
          <div className="d-flex justify-content-between align-items-center mb-1">
            <span className="fs-10 text-uppercase text-muted">
              {secondaryLabel}
            </span>
            <IconButton
              variant="outline-secondary"
              size="sm"
              icon={copiedField === 'secondary' ? faCheck : faCopy}
              onClick={() => copy('secondary', secondaryValue)}
            >
              {copiedField === 'secondary'
                ? t('common.secretOnce.copied')
                : t('common.secretOnce.copy')}
            </IconButton>
          </div>
          <div className="bg-body-tertiary p-2 rounded font-monospace text-break">
            {secondaryValue}
          </div>
        </div>
      )}

      <div className="d-flex justify-content-between align-items-center mb-1">
        <span className="fs-10 text-uppercase text-muted">{label}</span>
        <IconButton
          variant="outline-secondary"
          size="sm"
          icon={copiedField === 'secret' ? faCheck : faCopy}
          onClick={() => copy('secret', secret)}
        >
          {copiedField === 'secret'
            ? t('common.secretOnce.copied')
            : t('common.secretOnce.copy')}
        </IconButton>
      </div>
      <div className="bg-body-tertiary p-3 rounded font-monospace text-break mb-3">
        {secret}
      </div>

      <Form.Check
        type="checkbox"
        id={ackId}
        label={t('common.secretOnce.ackLabel')}
        checked={ack}
        onChange={e => onAckChange(e.target.checked)}
      />
    </>
  );
};

export default SecretOnceDisplay;
