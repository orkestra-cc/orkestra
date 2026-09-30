import { useEffect } from 'react';
import { Alert, Button, Form, ListGroup, Modal } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import { resolveErrorMessage } from 'helpers/errorMessage';
import { useAppSelector } from 'store/hooks';
import { selectUser } from 'store/slices/authSlice';
import {
  useApproveChangeRequestMutation,
  useGetCompliancePolicyQuery,
  useRejectChangeRequestMutation,
  type ChangeRequest
} from 'store/api/complianceApi';
import { diffPolicies, type DiffRow } from './policies/policyDiff';
import { errorBody, fieldLabel } from './policies/issueText';
import { formatDateTime } from './complianceFormat';

const noteSchema = yup.object({
  note: yup.string().trim().required().max(500)
});
type NoteForm = yup.InferType<typeof noteSchema>;

// PolicyDiffList shows field-by-field changes (before → after).
export const PolicyDiffList = ({ rows }: { rows: DiffRow[] }) => {
  const { t } = useTranslation();
  return (
    <ListGroup variant="flush" className="fs-10 border rounded">
      {rows.map(r => (
        <ListGroup.Item key={r.field} className="d-flex flex-wrap gap-2">
          <span className="fw-semibold text-900 me-auto">
            {fieldLabel(t, r.field)}
          </span>
          <span className="text-600 text-decoration-line-through">
            {r.before}
          </span>
          <span aria-hidden="true">→</span>
          <span className="text-900">{r.after}</span>
        </ListGroup.Item>
      ))}
    </ListGroup>
  );
};

interface Props {
  request: ChangeRequest | null;
  onHide: () => void;
}

// ChangeRequestModal shows a four-eyes request and lets an operator other
// than its author approve or reject it with a note (spec §1.5).
const ChangeRequestModal = ({ request, onHide }: Props) => {
  const { t } = useTranslation();
  const me = useAppSelector(selectUser);
  const currentId = request?.kind === 'update' ? request.policyUuid : undefined;
  const { data: current } = useGetCompliancePolicyQuery(currentId ?? '', {
    skip: !currentId
  });
  const [approve, { isLoading: approving }] = useApproveChangeRequestMutation();
  const [reject, { isLoading: rejecting }] = useRejectChangeRequestMutation();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors }
  } = useForm<NoteForm>({
    resolver: yupResolver(noteSchema),
    defaultValues: { note: '' }
  });

  useEffect(() => reset({ note: '' }), [request?.uuid, reset]);

  if (!request) return null;
  const isAuthor = me?.id === request.requestedBy;
  const canDecide = request.status === 'pending' && !isAuthor;
  const rows = request.payload.policy
    ? diffPolicies(current?.policy, request.payload.policy)
    : [];

  const decide = (action: 'approve' | 'reject') =>
    handleSubmit(async ({ note }) => {
      try {
        if (action === 'approve') {
          await approve({ id: request.uuid, note }).unwrap();
          toast.success(t('adminCompliance.changeRequests.approved'));
        } else {
          await reject({ id: request.uuid, note }).unwrap();
          toast.success(t('adminCompliance.changeRequests.rejected'));
        }
        onHide();
      } catch (err) {
        toast.error(
          resolveErrorMessage(
            errorBody(err),
            t('adminCompliance.changeRequests.decideError')
          )
        );
      }
    });

  return (
    <Modal show onHide={onHide} size="lg" centered>
      <Modal.Header closeButton>
        <Modal.Title as="h5">
          {t(`adminCompliance.changeRequests.kinds.${request.kind}`)}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <dl className="row fs-10 mb-3">
          <dt className="col-sm-3">
            {t('adminCompliance.changeRequests.requestedBy')}
          </dt>
          <dd className="col-sm-9 font-monospace">{request.requestedBy}</dd>
          <dt className="col-sm-3">
            {t('adminCompliance.changeRequests.requestedAt')}
          </dt>
          <dd className="col-sm-9">{formatDateTime(request.requestedAt)}</dd>
          {request.tenantId && (
            <>
              <dt className="col-sm-3">
                {t('adminCompliance.changeRequests.tenant')}
              </dt>
              <dd className="col-sm-9 font-monospace">{request.tenantId}</dd>
            </>
          )}
          <dt className="col-sm-3">
            {t('adminCompliance.changeRequests.reason')}
          </dt>
          <dd className="col-sm-9">{request.reason}</dd>
          <dt className="col-sm-3">
            {t('adminCompliance.changeRequests.status')}
          </dt>
          <dd className="col-sm-9">
            {t(`adminCompliance.changeRequests.statuses.${request.status}`)}
          </dd>
        </dl>
        {request.warnings.length > 0 && (
          <div className="mb-3">
            <h6 className="fs-10 text-700">
              {t('adminCompliance.changeRequests.warnings')}
            </h6>
            {request.warnings.map(code => (
              <SubtleBadge key={code} pill bg="warning" className="me-1">
                {t(`adminCompliance.issueTitles.${code}`, {
                  defaultValue: code
                })}
              </SubtleBadge>
            ))}
          </div>
        )}
        {rows.length > 0 && (
          <div className="mb-3">
            <h6 className="fs-10 text-700">
              {t('adminCompliance.changeRequests.changes')}
            </h6>
            <PolicyDiffList rows={rows} />
          </div>
        )}
        {(request.kind === 'assign' || request.kind === 'unassign') && (
          <p className="fs-10">
            {t(`adminCompliance.changeRequests.${request.kind}Summary`, {
              tenant: request.tenantId,
              from:
                request.payload.previousPolicyUuid ||
                t('adminCompliance.tenantCard.source.platform'),
              to: request.payload.policyUuid
            })}
          </p>
        )}
        {canDecide && (
          <Form.Group controlId="cr-note">
            <Form.Label className="fs-10">
              {t('adminCompliance.changeRequests.noteLabel')}
            </Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              isInvalid={!!errors.note}
              placeholder={t('adminCompliance.changeRequests.notePlaceholder')}
              {...register('note')}
            />
            <Form.Control.Feedback type="invalid">
              {t('adminCompliance.issues.reason_required')}
            </Form.Control.Feedback>
          </Form.Group>
        )}
        {isAuthor && request.status === 'pending' && (
          <Alert variant="info" className="fs-10 mb-0">
            {t('adminCompliance.changeRequests.ownRequest')}
          </Alert>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="orkestra-default" onClick={onHide}>
          {t('adminCompliance.changeRequests.close')}
        </Button>
        {canDecide && (
          <>
            <Button
              variant="orkestra-danger"
              onClick={decide('reject')}
              disabled={approving || rejecting}
            >
              {t('adminCompliance.changeRequests.reject')}
            </Button>
            <Button
              variant="orkestra-primary"
              onClick={decide('approve')}
              disabled={approving || rejecting}
            >
              {t('adminCompliance.changeRequests.approve')}
            </Button>
          </>
        )}
      </Modal.Footer>
    </Modal>
  );
};

export default ChangeRequestModal;
