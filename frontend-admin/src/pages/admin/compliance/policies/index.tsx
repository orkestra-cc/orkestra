import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Modal,
  Nav,
  Row,
  Spinner
} from 'react-bootstrap';
import {
  Link,
  useBlocker,
  useNavigate,
  useParams,
  useSearchParams
} from 'react-router';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import { resolveErrorMessage } from 'helpers/errorMessage';
import {
  useCreatePolicyMutation,
  useDeletePolicyMutation,
  useGetCompliancePolicyQuery,
  useListCompliancePoliciesQuery,
  useListRetentionClassesQuery,
  useUpdatePolicyMutation,
  useValidatePolicyMutation,
  type PolicyInput,
  type PolicyIssue
} from 'store/api/complianceApi';
import PolicyHistoryPanel from './PolicyHistoryPanel';
import PolicySaveModal from './PolicySaveModal';
import PolicySettingsForm from './PolicySettingsForm';
import PolicyTenantsPanel from './PolicyTenantsPanel';
import ReasonModal from './ReasonModal';
import { errorBody, issueText } from './issueText';
import { diffPolicies } from './policyDiff';
import {
  copyAsTenantPolicy,
  formFieldOf,
  newTenantPolicy,
  policySchema,
  toFormValues,
  toPolicyInput,
  type PolicyFormValues
} from './policyForm';
import { shouldBlockPolicyNavigation } from './policyNavigation';

const SECTIONS = ['settings', 'history', 'tenants'] as const;
type Section = (typeof SECTIONS)[number];

const editableOf = (p: PolicyInput): PolicyInput => ({
  name: p.name,
  description: p.description,
  logContent: p.logContent,
  retention: p.retention,
  sinks: p.sinks,
  accountability: p.accountability
});

// PolicyDetailPage edits one compliance policy (spec §8). The section lives in
// ?section= (no blocker on a section switch); ?from= seeds a new policy from
// an existing one. Saving validates first, then confirms in a modal with the
// diff, the warnings and a reason; a change with warnings may come back as a
// change request (202) instead of being applied.
const PolicyDetailPage = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { policyId = 'new' } = useParams<{ policyId: string }>();
  const isNew = policyId === 'new';
  const [searchParams, setSearchParams] = useSearchParams();
  const fromId = searchParams.get('from') ?? '';
  const rawSection = searchParams.get('section') as Section | null;
  const section: Section =
    !isNew && rawSection && SECTIONS.includes(rawSection)
      ? rawSection
      : 'settings';

  const detail = useGetCompliancePolicyQuery(policyId, { skip: isNew });
  const source = useGetCompliancePolicyQuery(fromId, {
    skip: !isNew || !fromId
  });
  const list = useListCompliancePoliciesQuery();
  const classes = useListRetentionClassesQuery();
  const platform = list.data?.items.find(p => p.isPlatformDefault);
  const policy = detail.data?.policy;
  const isPlatform = !!policy?.isPlatformDefault;

  const initial = useMemo<PolicyInput | undefined>(() => {
    if (!isNew) return policy ? editableOf(policy) : undefined;
    if (fromId) {
      const src = source.data?.policy;
      return src
        ? copyAsTenantPolicy(
            src,
            t('adminCompliance.policyDetail.copyName', { name: src.name })
          )
        : undefined;
    }
    return platform ? newTenantPolicy(platform) : undefined;
  }, [isNew, policy, fromId, source.data, platform, t]);

  const form = useForm<PolicyFormValues>({
    resolver: yupResolver(policySchema),
    values: initial ? toFormValues(initial) : undefined
  });
  const dirty = form.formState.isDirty;

  const [conflict, setConflict] = useState(false);
  const [review, setReview] = useState<{
    input: PolicyInput;
    warnings: PolicyIssue[];
  } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const skipBlock = useRef(false);
  const [validate, { isLoading: validating }] = useValidatePolicyMutation();
  const [create, { isLoading: creating }] = useCreatePolicyMutation();
  const [update, { isLoading: updating }] = useUpdatePolicyMutation();
  const [remove, { isLoading: removing }] = useDeletePolicyMutation();

  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      !skipBlock.current &&
      shouldBlockPolicyNavigation(dirty, currentLocation, nextLocation)
  );

  // The page instance survives new → /policies/<uuid>: re-arm the blocker
  // once that navigation has happened.
  useEffect(() => {
    skipBlock.current = false;
  }, [policyId]);

  const leave = (to: string) => {
    skipBlock.current = true;
    navigate(to, { replace: true });
  };

  const selectSection = (s: Section) =>
    setSearchParams(
      prev => {
        prev.set('section', s);
        return prev;
      },
      { replace: true }
    );

  const onSave = form.handleSubmit(async values => {
    const input = toPolicyInput(values, isPlatform ? policy?.sinks : undefined);
    try {
      const result = await validate({
        policyId: isNew ? undefined : policyId,
        policy: input
      }).unwrap();
      for (const issue of result.errors) {
        const field = formFieldOf(issue.field);
        if (field)
          form.setError(field, {
            type: 'server',
            message: issueText(t, issue)
          });
        else toast.error(issueText(t, issue));
      }
      if (result.errors.length === 0)
        setReview({ input, warnings: result.warnings });
    } catch (err) {
      toast.error(
        resolveErrorMessage(
          errorBody(err),
          t('adminCompliance.policyDetail.validateError')
        )
      );
    }
  });

  const onConfirm = async (reason: string, acknowledgeWarnings: boolean) => {
    if (!review) return;
    try {
      const res = isNew
        ? await create({
            policy: review.input,
            reason,
            acknowledgeWarnings
          }).unwrap()
        : await update({
            id: policyId,
            expectedVersion: policy!.version,
            policy: review.input,
            reason,
            acknowledgeWarnings
          }).unwrap();
      setReview(null);
      if (res.applied) {
        toast.success(t('adminCompliance.policyDetail.saved'));
        if (isNew && res.policy)
          leave(
            `/admin/compliance/policies/${encodeURIComponent(res.policy.uuid)}`
          );
        return;
      }
      toast.info(t('adminCompliance.policyDetail.sentForApproval'));
      if (isNew) leave('/admin/compliance?tab=changes');
      else if (initial) form.reset(toFormValues(initial));
    } catch (err) {
      setReview(null);
      const body = errorBody(err);
      if (body?.code === 'compliance.policy_version_conflict') {
        setConflict(true);
        return;
      }
      toast.error(
        resolveErrorMessage(body, t('adminCompliance.policyDetail.saveError'))
      );
    }
  };

  const onDelete = async (reason: string) => {
    if (!policy) return;
    try {
      await remove({
        id: policy.uuid,
        expectedVersion: policy.version,
        reason
      }).unwrap();
      toast.success(t('adminCompliance.policyDetail.deleted'));
      leave('/admin/compliance?tab=policies');
    } catch (err) {
      setDeleting(false);
      toast.error(
        resolveErrorMessage(
          errorBody(err),
          t('adminCompliance.policyDetail.deleteError')
        )
      );
    }
  };

  const reload = async () => {
    const res = await detail.refetch();
    if (!res.isError) setConflict(false);
  };

  if (detail.isError || source.isError) {
    return (
      <Alert variant="danger">
        {t('adminCompliance.policyDetail.loadError')}
      </Alert>
    );
  }
  if (!initial || !classes.data) {
    return <Spinner animation="border" size="sm" />;
  }

  const sections: Section[] = isNew ? ['settings'] : [...SECTIONS];
  const busy = validating || creating || updating;

  return (
    <>
      <Card className="mb-3">
        <Card.Body className="py-3 px-4 d-flex flex-wrap align-items-center gap-2">
          <div className="me-auto">
            <Link to="/admin/compliance?tab=policies" className="fs-10">
              {t('adminCompliance.policyDetail.back')}
            </Link>
            <h3 className="mb-0">
              {isNew
                ? t('adminCompliance.policyDetail.newTitle')
                : policy?.name}
              {isPlatform && (
                <SubtleBadge
                  pill
                  bg="primary"
                  className="ms-2 fs-10 align-middle"
                >
                  {t('adminCompliance.policies.badges.platform')}
                </SubtleBadge>
              )}
            </h3>
          </div>
          {!isNew && !isPlatform && (
            <Button
              variant="orkestra-danger"
              size="sm"
              onClick={() => setDeleting(true)}
              disabled={conflict}
            >
              {t('adminCompliance.policyDetail.delete')}
            </Button>
          )}
          {section === 'settings' && (
            <Button
              variant="orkestra-primary"
              size="sm"
              onClick={onSave}
              disabled={busy || conflict || (!dirty && !isNew)}
            >
              {busy
                ? t('adminCompliance.policyDetail.saving')
                : t('adminCompliance.policyDetail.save')}
            </Button>
          )}
        </Card.Body>
      </Card>

      {conflict && (
        <Alert variant="warning" className="d-flex align-items-center gap-3">
          <div className="me-auto">
            <div className="fw-semibold">
              {t('adminCompliance.policyDetail.conflictTitle')}
            </div>
            <div className="fs-10">
              {t('adminCompliance.policyDetail.conflictBody')}
            </div>
          </div>
          <Button variant="orkestra-default" size="sm" onClick={reload}>
            {t('adminCompliance.policyDetail.reload')}
          </Button>
        </Alert>
      )}

      <Row className="g-3">
        <Col md={3} lg={2}>
          <Nav
            className="flex-column"
            as="nav"
            aria-label={t('adminCompliance.policyDetail.newTitle')}
          >
            {sections.map(s => (
              <Nav.Link
                key={s}
                active={section === s}
                aria-current={section === s ? 'page' : undefined}
                onClick={() => selectSection(s)}
                className="px-2 py-1 fs-10"
              >
                {t(`adminCompliance.policyDetail.sections.${s}`)}
              </Nav.Link>
            ))}
          </Nav>
        </Col>
        <Col md={9} lg={10}>
          <Card className="shadow-none border">
            <Card.Body>
              {section === 'settings' && (
                <PolicySettingsForm
                  form={form}
                  isPlatform={isPlatform}
                  classes={classes.data.items}
                  platformRetention={platform?.retention ?? {}}
                  disabled={conflict}
                />
              )}
              {section === 'history' && !isNew && (
                <PolicyHistoryPanel policyId={policyId} />
              )}
              {section === 'tenants' && !isNew && (
                <PolicyTenantsPanel
                  assignments={detail.data?.assignments ?? []}
                />
              )}
            </Card.Body>
          </Card>
        </Col>
      </Row>

      <PolicySaveModal
        show={review !== null}
        rows={
          review ? diffPolicies(isNew ? undefined : initial, review.input) : []
        }
        warnings={review?.warnings ?? []}
        busy={creating || updating}
        onCancel={() => setReview(null)}
        onConfirm={onConfirm}
      />
      <ReasonModal
        show={deleting}
        title={t('adminCompliance.policyDetail.deleteTitle')}
        body={t('adminCompliance.policyDetail.deleteBody')}
        confirmLabel={t('adminCompliance.policyDetail.delete')}
        busy={removing}
        onCancel={() => setDeleting(false)}
        onConfirm={onDelete}
      />
      <Modal
        show={blocker.state === 'blocked'}
        onHide={() => blocker.reset?.()}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminCompliance.policyDetail.unsavedTitle')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body className="fs-10">
          {t('adminCompliance.policyDetail.unsavedBody')}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="orkestra-default" onClick={() => blocker.reset?.()}>
            {t('adminCompliance.policyDetail.stay')}
          </Button>
          <Button variant="orkestra-danger" onClick={() => blocker.proceed?.()}>
            {t('adminCompliance.policyDetail.leave')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default PolicyDetailPage;
