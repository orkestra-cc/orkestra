import { Alert, Button, Form } from 'react-bootstrap';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

interface Props {
  codes: string[];
  // ackRequired: when true an acknowledgement checkbox gates the
  // "Done" button. Used by the enroll wizard's terminal step where
  // the user must explicitly confirm they've saved the codes before
  // dismissing. The regenerate flow lands the codes inline on the
  // backup-codes tab and doesn't need a Done button — pass false.
  ackRequired?: boolean;
  onDone?: () => void;
  // Heading text — defaults to the localized "Backup codes". Override
  // when the surrounding context already establishes the heading.
  heading?: string;
}

// BackupCodesDisplay renders the one-shot backup-code list (10 codes,
// monospaced, in two columns) plus copy / download affordances.
// Extracted from MfaEnrollWizard step 3 so the same affordances are
// used by the self-service regenerate flow on /user/security.
const BackupCodesDisplay = ({
  codes,
  ackRequired = false,
  onDone,
  heading
}: Props) => {
  const { t } = useTranslation();
  const [ack, setAck] = useState(false);
  const resolvedHeading =
    heading ?? t('userSecurity.backupCodesDisplay.defaultHeading');
  return (
    <>
      <Alert variant="warning" className="mb-3">
        <strong>{t('userSecurity.backupCodesDisplay.savePrefix')}</strong>{' '}
        {t('userSecurity.backupCodesDisplay.saveBody')}
      </Alert>
      {resolvedHeading && <h6 className="mb-2">{resolvedHeading}</h6>}
      <div className="bg-body-tertiary p-3 rounded font-monospace mb-3">
        <div className="row g-2">
          {codes.map(c => (
            <div key={c} className="col-6 text-center">
              {c}
            </div>
          ))}
        </div>
      </div>
      <div className="d-flex justify-content-between mb-3">
        <Button
          variant="outline-secondary"
          size="sm"
          onClick={() => {
            navigator.clipboard.writeText(codes.join('\n'));
          }}
        >
          {t('userSecurity.backupCodesDisplay.copy')}
        </Button>
        <Button
          variant="outline-secondary"
          size="sm"
          onClick={() => {
            const blob = new Blob([codes.join('\n')], { type: 'text/plain' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = t('userSecurity.backupCodesDisplay.downloadFilename');
            a.click();
            URL.revokeObjectURL(url);
          }}
        >
          {t('userSecurity.backupCodesDisplay.download')}
        </Button>
      </div>
      {ackRequired && (
        <>
          <Form.Check
            type="checkbox"
            id="backup-codes-ack"
            label={t('userSecurity.backupCodesDisplay.ackLabel')}
            checked={ack}
            onChange={e => setAck(e.target.checked)}
          />
          <div className="d-flex justify-content-end mt-3">
            <Button variant="primary" disabled={!ack} onClick={onDone}>
              {t('userSecurity.backupCodesDisplay.done')}
            </Button>
          </div>
        </>
      )}
    </>
  );
};

export default BackupCodesDisplay;
