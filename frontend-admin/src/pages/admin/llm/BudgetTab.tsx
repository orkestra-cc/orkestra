import { useMemo, useState } from 'react';
import { Alert, Button, Col, Form, Row, Spinner } from 'react-bootstrap';
import { Controller, useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import MultiSelect from 'components/common/MultiSelect';
import { formatDateTime } from 'helpers/dateFormat';
import { useAppSelector } from 'store/hooks';
import { useListOrgMembersAdminQuery } from 'store/api/tenantApi';
import {
  LLM_BUDGET_ACTIONS,
  useGetLlmBudgetQuery,
  usePutLlmBudgetMutation,
  type LlmBudget,
  type LlmBudgetAction
} from 'store/api/llmApi';
import LlmLoadError from './LlmLoadError';
import { isReauthCancelled, llmErrorMessage } from './llmErrors';
import { useLlmPermissions } from './llmPermissions';

// Backend limits (models.LLMBudgetPutBody) and the module default for a
// new budget's warning threshold (budget_warn_pct).
const MAX_RECIPIENTS = 50;
const DEFAULT_WARN_PCT = 80;

interface BudgetFormValues {
  monthlyTokenLimit?: number;
  warnPct: number;
  action: LlmBudgetAction;
  notifyExtraUserUuids: string[];
}

// BudgetForm edits the org's monthly token budget. Mounted once per loaded
// budget (keyed by updatedAt), so its defaults are the stored values. The
// PUT is step-up-gated: baseApi's 401 interceptor drives the StepUpModal and
// replays it; a dismissed step-up ends quietly (isReauthCancelled).
const BudgetForm = ({ budget }: { budget: LlmBudget | null }) => {
  const { t } = useTranslation();
  const { canManageBudget: canManage } = useLlmPermissions();
  const [save, { isLoading: saving }] = usePutLlmBudgetMutation();
  const [error, setError] = useState<string | null>(null);
  const tenantId =
    useAppSelector(
      s => s.tenant.impersonatedTenantId ?? s.tenant.currentOrgId
    ) ?? '';

  const schema = useMemo(() => {
    const required = t('adminLlm.validation.required');
    const integer = t('adminLlm.validation.integer');
    return yup.object({
      monthlyTokenLimit: yup
        .number()
        .transform((v, orig) =>
          orig === '' || orig === null || Number.isNaN(v) ? undefined : v
        )
        .required(required)
        .integer(integer)
        .min(1, t('adminLlm.validation.min', { min: 1 }))
        .max(
          Number.MAX_SAFE_INTEGER,
          t('adminLlm.validation.max', { max: Number.MAX_SAFE_INTEGER })
        ),
      warnPct: yup
        .number()
        .typeError(t('adminLlm.validation.number'))
        .required(required)
        .integer(integer)
        .min(0, t('adminLlm.validation.min', { min: 0 }))
        .max(100, t('adminLlm.validation.max', { max: 100 })),
      action: yup
        .mixed<LlmBudgetAction>()
        .oneOf(LLM_BUDGET_ACTIONS)
        .required(required),
      notifyExtraUserUuids: yup
        .array()
        .of(yup.string().required())
        .max(
          MAX_RECIPIENTS,
          t('adminLlm.budget.validation.maxRecipients', {
            max: MAX_RECIPIENTS
          })
        )
        .required()
    });
  }, [t]);

  const {
    register,
    handleSubmit,
    watch,
    control,
    formState: { errors }
  } = useForm<BudgetFormValues>({
    resolver: yupResolver(schema) as unknown as Resolver<BudgetFormValues>,
    defaultValues: {
      monthlyTokenLimit: budget?.monthlyTokenLimit,
      warnPct: budget?.warnPct ?? DEFAULT_WARN_PCT,
      action: budget?.action ?? 'block',
      notifyExtraUserUuids: budget?.notifyExtraUserUuids ?? []
    }
  });
  const action = watch('action');
  const recipients = watch('notifyExtraUserUuids');

  // GET /v1/admin/tenants/{id}/members needs system.tenants.admin on top of
  // llm.budgets.admin — the same limit as the model grant picker.
  const members = useListOrgMembersAdminQuery(tenantId, {
    skip: !tenantId || !canManage
  });
  const emailOf = useMemo(
    () =>
      new Map((members.data?.members ?? []).map(m => [m.userUUID, m.email])),
    [members.data]
  );
  const memberOptions = useMemo(() => {
    const known = (members.data?.members ?? []).map(m => ({
      value: m.userUUID,
      label: m.email || m.userUUID
    }));
    // A stored recipient missing from a loaded member list stays visible
    // so the operator can drop it; before the list loads it shows by id.
    const unknown = recipients
      .filter(id => !known.some(o => o.value === id))
      .map(id => ({
        value: id,
        label: members.isSuccess
          ? t('adminLlm.budget.fields.formerMember', { id })
          : id
      }));
    return [...known, ...unknown];
  }, [members.data, members.isSuccess, recipients, t]);
  const membersForbidden =
    members.isError &&
    (members.error as { status?: number } | undefined)?.status === 403;

  const onSubmit = async (values: BudgetFormValues) => {
    setError(null);
    try {
      await save({
        monthlyTokenLimit: values.monthlyTokenLimit as number,
        warnPct: values.warnPct,
        action: values.action,
        notifyExtraUserUuids: values.notifyExtraUserUuids
      }).unwrap();
      toast.success(t('adminLlm.budget.saveSuccess'));
    } catch (e) {
      if (!isReauthCancelled(e)) setError(llmErrorMessage(t, e));
    }
  };

  return (
    <Form noValidate onSubmit={handleSubmit(onSubmit)}>
      <p className="fs-10 text-600">{t('adminLlm.budget.semantics')}</p>
      {budget ? (
        <p className="fs-10 text-600">
          {t('adminLlm.budget.lastUpdated', {
            date: formatDateTime(budget.updatedAt),
            user: emailOf.get(budget.updatedBy) || budget.updatedBy
          })}
        </p>
      ) : (
        <Alert variant="info" className="fs-10 py-2">
          {t('adminLlm.budget.none')}
        </Alert>
      )}
      {!canManage && (
        <Alert variant="secondary" className="fs-10 py-2">
          {t('adminLlm.budget.noPermission')}
        </Alert>
      )}
      {error && (
        <Alert variant="danger" className="fs-10 py-2">
          {error}
        </Alert>
      )}
      <fieldset disabled={!canManage}>
        <Row className="g-3">
          <Col md={6}>
            <Form.Group controlId="llm-budget-limit">
              <Form.Label>{t('adminLlm.budget.fields.limit')}</Form.Label>
              <Form.Control
                type="number"
                min={1}
                step={1}
                isInvalid={!!errors.monthlyTokenLimit}
                {...register('monthlyTokenLimit')}
              />
              <Form.Control.Feedback type="invalid">
                {errors.monthlyTokenLimit?.message}
              </Form.Control.Feedback>
              <Form.Text className="text-600">
                {t('adminLlm.budget.fields.limitHint')}
              </Form.Text>
            </Form.Group>
          </Col>
          <Col md={6}>
            <Form.Group controlId="llm-budget-warn">
              <Form.Label>{t('adminLlm.budget.fields.warnPct')}</Form.Label>
              <Form.Control
                type="number"
                min={0}
                max={100}
                step={1}
                isInvalid={!!errors.warnPct}
                {...register('warnPct')}
              />
              <Form.Control.Feedback type="invalid">
                {errors.warnPct?.message}
              </Form.Control.Feedback>
              <Form.Text className="text-600">
                {t('adminLlm.budget.fields.warnPctHint')}
              </Form.Text>
            </Form.Group>
          </Col>
          <Col md={12}>
            <Form.Group controlId="llm-budget-action">
              <Form.Label>{t('adminLlm.budget.fields.action')}</Form.Label>
              <Form.Select {...register('action')}>
                {LLM_BUDGET_ACTIONS.map(a => (
                  <option key={a} value={a}>
                    {t(`adminLlm.budget.actions.${a}`)}
                  </option>
                ))}
              </Form.Select>
              <Form.Text className="text-600">
                {t(`adminLlm.budget.actionHints.${action}`)}
              </Form.Text>
            </Form.Group>
          </Col>
          <Col md={12}>
            <Form.Group>
              <Form.Label htmlFor="llm-budget-recipients">
                {t('adminLlm.budget.fields.recipients')}
              </Form.Label>
              <Controller
                control={control}
                name="notifyExtraUserUuids"
                render={({ field }) => (
                  <MultiSelect
                    inputId="llm-budget-recipients"
                    options={memberOptions}
                    isDisabled={!canManage}
                    isLoading={members.isLoading}
                    value={memberOptions.filter(o =>
                      field.value.includes(o.value)
                    )}
                    onChange={sel =>
                      field.onChange(
                        ((sel ?? []) as { value: string }[]).map(o => o.value)
                      )
                    }
                    onBlur={field.onBlur}
                    placeholder={t(
                      'adminLlm.budget.fields.recipientsPlaceholder'
                    )}
                    noOptionsMessage={() =>
                      t('adminLlm.budget.fields.noMembers')
                    }
                  />
                )}
              />
              {errors.notifyExtraUserUuids && (
                <Form.Text className="d-block text-danger">
                  {errors.notifyExtraUserUuids.message}
                </Form.Text>
              )}
              {members.isError && (
                <Form.Text className="d-block text-danger">
                  {membersForbidden
                    ? t('adminLlm.budget.fields.membersForbidden')
                    : t('adminLlm.budget.fields.membersError')}
                </Form.Text>
              )}
              <Form.Text className="d-block text-600">
                {t('adminLlm.budget.fields.recipientsHint')}
              </Form.Text>
            </Form.Group>
          </Col>
        </Row>
      </fieldset>
      <div className="d-flex justify-content-end mt-3">
        <Button
          variant="orkestra-primary"
          type="submit"
          disabled={!canManage || saving}
        >
          {t('adminLlm.actions.save')}
        </Button>
      </div>
    </Form>
  );
};

// BudgetTab reads the budget (llm.admin.read; 204 = none, no limit) and
// hands it to the form; saving needs llm.budgets.admin.
const BudgetTab = () => {
  const tenantId =
    useAppSelector(
      s => s.tenant.impersonatedTenantId ?? s.tenant.currentOrgId
    ) ?? '';
  // Keyed by org: right after a switch currentData is undefined until the
  // new org's budget lands, so the form never shows (or saves) the
  // previous org's values.
  const { currentData, isError, refetch } = useGetLlmBudgetQuery(tenantId);
  if (isError) return <LlmLoadError onRetry={() => refetch()} />;
  if (currentData === undefined)
    return <Spinner animation="border" size="sm" />;
  const budget = currentData;
  return (
    <BudgetForm
      key={`${tenantId}:${budget?.updatedAt ?? 'none'}`}
      budget={budget}
    />
  );
};

export default BudgetTab;
