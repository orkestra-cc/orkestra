import { useEffect, useMemo } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import type { PolicyIssue } from 'store/api/complianceApi';
import { PolicyDiffList } from '../ChangeRequestModal';
import { issueText } from './issueText';
import type { DiffRow } from './policyDiff';

interface Props {
  show: boolean;
  rows: DiffRow[];
  warnings: PolicyIssue[];
  busy: boolean;
  onCancel: () => void;
  onConfirm: (reason: string, acknowledgeWarnings: boolean) => void;
}

interface ConfirmForm {
  reason: string;
  acknowledge: boolean;
}

// PolicySaveModal is the last step of a policy change (spec §8): the diff,
// the warnings, a mandatory reason and, with warnings, their explicit
// acknowledgement.
const PolicySaveModal = ({
  show,
  rows,
  warnings,
  busy,
  onCancel,
  onConfirm
}: Props) => {
  const { t } = useTranslation();
  const hasWarnings = warnings.length > 0;
  const schema = useMemo(
    () =>
      yup.object({
        reason: yup.string().trim().required().max(500),
        acknowledge: yup
          .boolean()
          .defined()
          .test('acknowledged', 'acknowledge', v => !hasWarnings || v === true)
      }),
    [hasWarnings]
  );
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<ConfirmForm>({
    resolver: yupResolver(schema),
    defaultValues: { reason: '', acknowledge: false }
  });

  useEffect(() => {
    if (show) reset({ reason: '', acknowledge: false });
  }, [show, reset]);

  return (
    <Modal show={show} onHide={onCancel} size="lg" centered backdrop="static">
      <Form
        noValidate
        onSubmit={handleSubmit(v =>
          onConfirm(v.reason.trim(), hasWarnings && v.acknowledge)
        )}
      >
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminCompliance.saveModal.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <h6 className="fs-10 text-700">
            {t('adminCompliance.saveModal.changes')}
          </h6>
          {rows.length > 0 ? (
            <PolicyDiffList rows={rows} />
          ) : (
            <p className="fs-10 text-600">
              {t('adminCompliance.saveModal.noChanges')}
            </p>
          )}
          {hasWarnings && (
            <Alert variant="warning" className="mt-3 fs-10">
              <div className="fw-semibold mb-1">
                {t('adminCompliance.saveModal.warnings')}
              </div>
              <ul className="mb-2 ps-3">
                {warnings.map((w, i) => (
                  <li key={`${w.code}-${i}`}>{issueText(t, w)}</li>
                ))}
              </ul>
              <div>{t('adminCompliance.saveModal.fourEyesNote')}</div>
            </Alert>
          )}
          <Form.Group controlId="policy-reason" className="mt-3">
            <Form.Label>
              {t('adminCompliance.saveModal.reasonLabel')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              placeholder={t('adminCompliance.saveModal.reasonPlaceholder')}
              isInvalid={!!errors.reason}
              {...register('reason')}
            />
            <Form.Control.Feedback type="invalid">
              {t('adminCompliance.issues.reason_required')}
            </Form.Control.Feedback>
          </Form.Group>
          {hasWarnings && (
            <Form.Check
              id="policy-ack"
              className="mt-2"
              label={t('adminCompliance.saveModal.acknowledge')}
              isInvalid={!!errors.acknowledge}
              feedback={t('adminCompliance.saveModal.acknowledgeRequired')}
              feedbackType="invalid"
              {...register('acknowledge')}
            />
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={onCancel} disabled={busy}>
            {t('adminCompliance.saveModal.cancel')}
          </Button>
          <Button type="submit" variant="orkestra-primary" disabled={busy}>
            {t('adminCompliance.saveModal.confirm')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default PolicySaveModal;
