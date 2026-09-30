import { useEffect, useRef } from 'react';
import { Alert, Button, Form, Modal } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { resolveErrorMessage } from 'helpers/errorMessage';
import {
  useAssignPolicyMutation,
  useListCompliancePoliciesQuery,
  useUnassignPolicyMutation,
  useValidateAssignmentMutation
} from 'store/api/complianceApi';
import {
  errorBody,
  issueText
} from 'pages/admin/compliance/policies/issueText';

const schema = yup.object({
  policyId: yup.string().defined(),
  reason: yup.string().trim().required().max(500),
  acknowledge: yup.boolean().defined()
});
type AssignForm = yup.InferType<typeof schema>;

interface Props {
  show: boolean;
  tenantId: string;
  currentPolicyId: string; // '' = platform policy
  onHide: () => void;
}

// AssignPolicyModal changes the tenant's compliance policy (spec §8): the
// warnings of the move are previewed server-side, acknowledged, and the
// change may come back as a four-eyes request.
const AssignPolicyModal = ({
  show,
  tenantId,
  currentPolicyId,
  onHide
}: Props) => {
  const { t } = useTranslation();
  const { data: policies } = useListCompliancePoliciesQuery(undefined, {
    skip: !show
  });
  const [preview, previewResult] = useValidateAssignmentMutation();
  const [assign, { isLoading: assigning }] = useAssignPolicyMutation();
  const [unassign, { isLoading: unassigning }] = useUnassignPolicyMutation();
  const {
    register,
    handleSubmit,
    reset,
    watch,
    setError,
    formState: { errors }
  } = useForm<AssignForm>({
    resolver: yupResolver(schema),
    defaultValues: { policyId: currentPolicyId, reason: '', acknowledge: false }
  });
  const policyId = watch('policyId');
  const unchanged = policyId === currentPolicyId;
  // RTK's mutation `reset` changes identity as the request progresses; kept in
  // a ref so the re-seed below runs on open / policy change only, never on the
  // preview's own state transitions (which would undo the operator's choice).
  const resetPreview = useRef(previewResult.reset);
  resetPreview.current = previewResult.reset;

  useEffect(() => {
    if (!show) return;
    reset({ policyId: currentPolicyId, reason: '', acknowledge: false });
    resetPreview.current();
  }, [show, currentPolicyId, reset]);

  useEffect(() => {
    if (show && !unchanged)
      void preview({ tenantId, policyId: policyId || undefined });
  }, [show, unchanged, tenantId, policyId, preview]);

  // Warnings count only for the selection they were computed for.
  const previewReady =
    !unchanged &&
    (previewResult.originalArgs?.policyId ?? '') === policyId &&
    !!previewResult.data;
  const warnings = previewReady ? previewResult.data!.warnings : [];

  const onSubmit = handleSubmit(async v => {
    if (warnings.length > 0 && !v.acknowledge) {
      setError('acknowledge', { type: 'required' });
      return;
    }
    const body = {
      reason: v.reason.trim(),
      acknowledgeWarnings: warnings.length > 0
    };
    try {
      const res = v.policyId
        ? await assign({ tenantId, policyId: v.policyId, ...body }).unwrap()
        : await unassign({ tenantId, ...body }).unwrap();
      toast.success(
        res.applied
          ? t('adminCompliance.assignModal.applied')
          : t('adminCompliance.assignModal.sentForApproval')
      );
      onHide();
    } catch (err) {
      toast.error(
        resolveErrorMessage(
          errorBody(err),
          t('adminCompliance.assignModal.error')
        )
      );
    }
  });

  return (
    <Modal show={show} onHide={onHide} centered>
      <Form noValidate onSubmit={onSubmit}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminCompliance.assignModal.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form.Group controlId="assign-policy" className="mb-3">
            <Form.Label>
              {t('adminCompliance.assignModal.policyLabel')}
            </Form.Label>
            <Form.Select {...register('policyId')}>
              <option value="">
                {t('adminCompliance.assignModal.platformOption')}
              </option>
              {(policies?.items ?? [])
                .filter(p => !p.isPlatformDefault)
                .map(p => (
                  <option key={p.uuid} value={p.uuid}>
                    {p.name}
                  </option>
                ))}
            </Form.Select>
          </Form.Group>
          {previewReady &&
            (warnings.length > 0 ? (
              <Alert variant="warning" className="fs-10">
                <ul className="mb-0 ps-3">
                  {warnings.map((w, i) => (
                    <li key={`${w.code}-${i}`}>{issueText(t, w)}</li>
                  ))}
                </ul>
              </Alert>
            ) : (
              <p className="fs-10 text-600">
                {t('adminCompliance.assignModal.noWarnings')}
              </p>
            ))}
          <Form.Group controlId="assign-reason">
            <Form.Label>
              {t('adminCompliance.assignModal.reasonLabel')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              isInvalid={!!errors.reason}
              {...register('reason')}
            />
            <Form.Control.Feedback type="invalid">
              {t('adminCompliance.issues.reason_required')}
            </Form.Control.Feedback>
          </Form.Group>
          {warnings.length > 0 && (
            <Form.Check
              id="assign-ack"
              className="mt-2"
              label={t('adminCompliance.assignModal.acknowledge')}
              isInvalid={!!errors.acknowledge}
              feedback={t('adminCompliance.saveModal.acknowledgeRequired')}
              feedbackType="invalid"
              {...register('acknowledge')}
            />
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={onHide}>
            {t('adminCompliance.assignModal.cancel')}
          </Button>
          <Button
            type="submit"
            variant="orkestra-primary"
            disabled={!previewReady || assigning || unassigning}
          >
            {t('adminCompliance.assignModal.confirm')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  );
};

export default AssignPolicyModal;
