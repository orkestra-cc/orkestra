import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Col, Form, Modal, Row } from 'react-bootstrap';
import { useFieldArray, useForm, type Resolver } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import { faMinus, faPlus } from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import IconButton from 'components/common/IconButton';
import MultiSelect from 'components/common/MultiSelect';
import { useAppSelector } from 'store/hooks';
import { useListOrgMembersAdminQuery } from 'store/api/tenantApi';
import {
  LLM_PROVIDERS,
  useCreateLlmModelMutation,
  usePatchLlmModelMutation,
  usePutLlmGrantsMutation,
  type LlmAccess,
  type LlmCredential,
  type LlmModel,
  type LlmModelBody,
  type LlmModelPatch,
  type LlmProvider
} from 'store/api/llmApi';
import { isReauthCancelled, llmErrorMessage } from './llmErrors';
import { useLlmPermissions } from './llmPermissions';
import { LLM_MODEL_ID_RE, LLM_NAME_RE, LLM_PURPOSE_RE } from './llmRules';

type Effort = '' | 'low' | 'medium' | 'high';
type CredentialKind = 'org' | 'user_account';
const EFFORTS: Effort[] = ['', 'low', 'medium', 'high'];

interface ModelFormValues {
  name: string;
  provider: LlmProvider;
  modelId: string;
  credentialKind: CredentialKind;
  credentialUuid: string;
  chat: boolean;
  streaming: boolean;
  tools: boolean;
  structuredOutput: boolean;
  embeddings: boolean;
  dimensions?: number;
  temperature?: number;
  maxOutputTokens?: number;
  effort: Effort;
  purposes: { purpose: string; priority: number }[];
  access: LlmAccess;
}

const toFormValues = (m?: LlmModel): ModelFormValues => ({
  name: m?.name ?? '',
  provider: m?.provider ?? 'openai',
  modelId: m?.modelId ?? '',
  credentialKind: m?.credentialRef.kind ?? 'org',
  credentialUuid: m?.credentialRef.credentialUuid ?? '',
  chat: m?.capabilities.chat ?? true,
  streaming: m?.capabilities.streaming ?? true,
  tools: m?.capabilities.tools ?? false,
  structuredOutput: m?.capabilities.structuredOutput ?? false,
  embeddings: m?.capabilities.embeddings ?? false,
  dimensions: m?.capabilities.dimensions,
  temperature: m?.defaults.temperature,
  maxOutputTokens: m?.defaults.maxOutputTokens,
  effort: m?.defaults.effort ?? '',
  purposes: m?.purposes.map(p => ({ ...p })) ?? [
    { purpose: 'default', priority: 10 }
  ],
  access: m?.access ?? 'granted'
});

// toBody turns form values into the wire body. A user_account model (billed
// to each user's own ChatGPT plan) carries no credential, no embeddings and
// no sampling defaults — the backend rejects all three on that route.
const toBody = (v: ModelFormValues): LlmModelBody => {
  const userAccount = v.credentialKind === 'user_account';
  const embeddings = userAccount ? false : v.embeddings;
  return {
    name: v.name,
    provider: v.provider,
    modelId: v.modelId,
    capabilities: {
      chat: v.chat,
      streaming: v.streaming,
      tools: v.tools,
      structuredOutput: v.structuredOutput,
      embeddings,
      ...(embeddings && v.dimensions ? { dimensions: v.dimensions } : {})
    },
    credentialRef: userAccount
      ? { kind: 'user_account' }
      : { kind: 'org', credentialUuid: v.credentialUuid },
    defaults: userAccount
      ? { effort: v.effort }
      : {
          ...(v.temperature !== undefined
            ? { temperature: v.temperature }
            : {}),
          ...(v.maxOutputTokens !== undefined
            ? { maxOutputTokens: v.maxOutputTokens }
            : {}),
          effort: v.effort
        },
    purposes: v.purposes.map(p => ({
      purpose: p.purpose,
      priority: p.priority
    }))
  };
};

// The model PATCH is partial: send only the top-level fields whose value
// differs from the stored model (both sides normalised through toBody).
const diffBody = (next: LlmModelBody, prev: LlmModelBody): LlmModelPatch => {
  const patch: Record<string, unknown> = {};
  (Object.keys(next) as (keyof LlmModelBody)[]).forEach(key => {
    if (JSON.stringify(next[key]) !== JSON.stringify(prev[key])) {
      patch[key] = next[key];
    }
  });
  return patch as LlmModelPatch;
};

const sameSet = (a: string[], b: string[]) =>
  a.length === b.length && a.every(x => b.includes(x));

// An empty number input arrives as '' → NaN; treat it as "not set".
const optionalNumber = () =>
  yup
    .number()
    .transform((v, orig) =>
      orig === '' || orig === null || Number.isNaN(v) ? undefined : v
    );

interface Props {
  model?: LlmModel;
  grantsOnly?: boolean;
  credentials: LlmCredential[];
  onClose: () => void;
}

// One modal for the model definition and its grants. For a user_account
// model embeddings, temperature and max tokens are locked: they are shown
// as inert read-only controls (never a disabled registered input, whose
// value react-hook-form would drop) and left out of the body. The grant
// list is a second, step-up-gated PUT: when it fails after the model was
// saved, the operator is told the grants were not saved.
const ModelEditorModal = ({
  model,
  grantsOnly,
  credentials,
  onClose
}: Props) => {
  const { t } = useTranslation();
  const { canManageGrants } = useLlmPermissions();
  const tenantId =
    useAppSelector(
      s => s.tenant.impersonatedTenantId ?? s.tenant.currentOrgId
    ) ?? '';
  const [create, { isLoading: creating }] = useCreateLlmModelMutation();
  const [patch, { isLoading: patching }] = usePatchLlmModelMutation();
  const [putGrants, { isLoading: granting }] = usePutLlmGrantsMutation();
  const originalGrantees = useMemo(
    () => model?.grants.map(g => g.userUuid) ?? [],
    [model]
  );
  const [grantees, setGrantees] = useState<string[]>(originalGrantees);
  const [error, setError] = useState<string | null>(null);

  const schema = useMemo(() => {
    const required = t('adminLlm.validation.required');
    return yup.object({
      name: yup
        .string()
        .trim()
        .required(required)
        .matches(LLM_NAME_RE, t('adminLlm.validation.name')),
      provider: yup.mixed<LlmProvider>().oneOf(LLM_PROVIDERS).required(),
      modelId: yup
        .string()
        .trim()
        .required(required)
        .matches(LLM_MODEL_ID_RE, t('adminLlm.validation.modelId')),
      credentialKind: yup
        .mixed<CredentialKind>()
        .oneOf(['org', 'user_account'])
        .required(),
      credentialUuid: yup.string().when('credentialKind', {
        is: 'org',
        then: s => s.required(required),
        otherwise: s => s.optional()
      }),
      chat: yup.boolean().required(),
      streaming: yup.boolean().required(),
      tools: yup.boolean().required(),
      structuredOutput: yup.boolean().required(),
      embeddings: yup.boolean().required(),
      dimensions: optionalNumber().when(['embeddings', 'credentialKind'], {
        is: (e: boolean, k: CredentialKind) => e && k === 'org',
        then: s =>
          s
            .required(required)
            .integer(t('adminLlm.validation.integer'))
            .min(1, t('adminLlm.validation.min', { min: 1 })),
        otherwise: s => s.optional()
      }),
      temperature: optionalNumber()
        .min(0, t('adminLlm.validation.min', { min: 0 }))
        .max(2, t('adminLlm.validation.max', { max: 2 })),
      maxOutputTokens: optionalNumber()
        .integer(t('adminLlm.validation.integer'))
        .min(1, t('adminLlm.validation.min', { min: 1 })),
      effort: yup.mixed<Effort>().oneOf(EFFORTS).required(),
      purposes: yup
        .array()
        .of(
          yup.object({
            purpose: yup
              .string()
              .trim()
              .required(required)
              .matches(LLM_PURPOSE_RE, t('adminLlm.validation.purpose')),
            priority: yup
              .number()
              .typeError(t('adminLlm.validation.number'))
              .integer(t('adminLlm.validation.integer'))
              .min(0, t('adminLlm.validation.min', { min: 0 }))
              .required(required)
          })
        )
        .min(1)
        .max(16)
        .required(),
      access: yup.mixed<LlmAccess>().oneOf(['granted', 'everyone']).required()
    });
  }, [t]);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    control,
    formState: { errors }
  } = useForm<ModelFormValues>({
    resolver: yupResolver(schema) as unknown as Resolver<ModelFormValues>,
    defaultValues: toFormValues(model)
  });
  const { fields, append, remove } = useFieldArray({
    control,
    name: 'purposes'
  });
  const provider = watch('provider');
  const credentialKind = watch('credentialKind');
  const credentialUuid = watch('credentialUuid');
  const access = watch('access');
  const embeddings = watch('embeddings');
  const isUserAccount = credentialKind === 'user_account';
  const showGrants = access === 'granted';

  // Keep dependent fields coherent: user_account is OpenAI-only and has no
  // embeddings; a credential of another provider cannot stay selected.
  useEffect(() => {
    if (provider !== 'openai' && credentialKind === 'user_account') {
      setValue('credentialKind', 'org');
    }
  }, [provider, credentialKind, setValue]);
  useEffect(() => {
    if (isUserAccount) setValue('embeddings', false);
  }, [isUserAccount, setValue]);

  const credentialOptions = useMemo(
    () =>
      credentials.filter(
        c =>
          c.provider === provider &&
          (c.status === 'active' ||
            c.uuid === model?.credentialRef.credentialUuid)
      ),
    [credentials, provider, model]
  );
  useEffect(() => {
    if (
      credentialUuid &&
      !credentialOptions.some(c => c.uuid === credentialUuid)
    ) {
      setValue('credentialUuid', '');
    }
  }, [credentialOptions, credentialUuid, setValue]);

  const members = useListOrgMembersAdminQuery(tenantId, {
    skip: !tenantId || !canManageGrants || !showGrants
  });
  const memberOptions = useMemo(() => {
    const known = (members.data?.members ?? []).map(m => ({
      value: m.userUUID,
      label: m.email || m.userUUID
    }));
    // A grantee missing from the member list stays visible (so the operator
    // can see and drop it) instead of vanishing silently. Only a list that
    // actually loaded can prove someone left: while it is loading, skipped
    // (no llm.grants.admin) or refused, the grantee is shown by id alone.
    const unknown = grantees
      .filter(id => !known.some(o => o.value === id))
      .map(id => ({
        value: id,
        label: members.isSuccess
          ? t('adminLlm.models.fields.formerMember', { id })
          : id
      }));
    return [...known, ...unknown];
  }, [members.data, members.isSuccess, grantees, t]);
  // GET /v1/admin/tenants/{id}/members needs system.tenants.admin on top of
  // llm.grants.admin — a known PR 1 limitation; say so instead of a bare error.
  const membersForbidden =
    members.isError &&
    (members.error as { status?: number } | undefined)?.status === 403;

  // Access and grants travel together on the grants route
  // (llm.grants.admin + step-up); the model PATCH never carries access. With
  // access 'everyone' the current grantees are sent unchanged and stay
  // dormant on the backend.
  // null when saved; otherwise the reason to show, or '' when the operator
  // dismissed the step-up (nothing more to explain).
  const saveGrants = async (
    uuid: string,
    nextAccess: LlmAccess
  ): Promise<string | null> => {
    try {
      await putGrants({
        uuid,
        access: nextAccess,
        userUuids: grantees
      }).unwrap();
      return null;
    } catch (e) {
      return isReauthCancelled(e) ? '' : llmErrorMessage(t, e);
    }
  };

  const onSubmit = async (values: ModelFormValues) => {
    setError(null);
    const body = toBody(values);
    let savedUuid: string;
    try {
      if (model) {
        const changes = diffBody(body, toBody(toFormValues(model)));
        if (Object.keys(changes).length > 0) {
          await patch({ uuid: model.uuid, ...changes }).unwrap();
        }
        savedUuid = model.uuid;
      } else {
        savedUuid = (await create(body).unwrap()).uuid;
      }
    } catch (e) {
      if (!isReauthCancelled(e)) setError(llmErrorMessage(t, e));
      return;
    }
    // A new model is created closed (access granted, no grants).
    const storedAccess: LlmAccess = model?.access ?? 'granted';
    const accessChanged = values.access !== storedAccess;
    const grantsChanged =
      values.access === 'granted' && !sameSet(grantees, originalGrantees);
    const grantsFailure =
      canManageGrants && (accessChanged || grantsChanged)
        ? await saveGrants(savedUuid, values.access)
        : null;
    if (grantsFailure !== null) {
      toast.warning(
        grantsFailure
          ? t('adminLlm.models.grantsNotSavedReason', {
              reason: grantsFailure
            })
          : t('adminLlm.models.grantsNotSaved')
      );
      onClose();
      return;
    }
    toast.success(t('adminLlm.models.saveSuccess'));
    onClose();
  };

  const onSaveGrantsOnly = async () => {
    if (!model) return;
    setError(null);
    try {
      await putGrants({
        uuid: model.uuid,
        access,
        userUuids: grantees
      }).unwrap();
      toast.success(t('adminLlm.models.grantsSaved'));
      onClose();
    } catch (e) {
      if (!isReauthCancelled(e)) setError(llmErrorMessage(t, e));
    }
  };

  const errorAlert = error && (
    <Alert variant="danger" className="fs-10 py-2">
      {error}
    </Alert>
  );

  // Access is decided with the grants (llm.grants.admin), not with the model.
  const accessSelect = (
    <Form.Group className="mb-3" controlId="llm-model-access">
      <Form.Label>{t('adminLlm.models.fields.access')}</Form.Label>
      <Form.Select disabled={!canManageGrants} {...register('access')}>
        <option value="granted">{t('adminLlm.access.granted')}</option>
        <option value="everyone">{t('adminLlm.access.everyone')}</option>
      </Form.Select>
      {access === 'everyone' && (
        <Form.Text className="d-block text-600">
          {t('adminLlm.models.fields.accessEveryoneHint')}
        </Form.Text>
      )}
    </Form.Group>
  );

  const grantsPicker = (
    <Form.Group className="mb-3">
      <Form.Label htmlFor="llm-model-grantees">
        {t('adminLlm.models.fields.grantees')}
      </Form.Label>
      <MultiSelect
        inputId="llm-model-grantees"
        options={memberOptions}
        isDisabled={!canManageGrants}
        isLoading={members.isLoading}
        value={memberOptions.filter(o => grantees.includes(o.value))}
        onChange={sel =>
          setGrantees(((sel ?? []) as { value: string }[]).map(o => o.value))
        }
        placeholder={t('adminLlm.models.fields.granteesPlaceholder')}
        noOptionsMessage={() => t('adminLlm.models.fields.noMembers')}
      />
      {members.isError && (
        <Form.Text className="d-block text-danger">
          {membersForbidden
            ? t('adminLlm.models.fields.membersForbidden')
            : t('adminLlm.models.fields.membersError')}
        </Form.Text>
      )}
      <Form.Text className="d-block text-600">
        {canManageGrants
          ? t('adminLlm.models.fields.granteesHint')
          : t('adminLlm.models.fields.granteesNoPermission')}
      </Form.Text>
    </Form.Group>
  );

  const footer = (onSave?: () => void) => (
    <Modal.Footer>
      <Button variant="orkestra-default" onClick={onClose}>
        {t('adminLlm.actions.cancel')}
      </Button>
      <Button
        variant="orkestra-primary"
        type={onSave ? 'button' : 'submit'}
        onClick={onSave}
        disabled={creating || patching || granting}
      >
        {t('adminLlm.actions.save')}
      </Button>
    </Modal.Footer>
  );

  if (grantsOnly && model) {
    return (
      <Modal show onHide={onClose} centered>
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {t('adminLlm.models.grantsTitle', { name: model.name })}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {errorAlert}
          {accessSelect}
          {access === 'granted' && grantsPicker}
        </Modal.Body>
        {footer(onSaveGrantsOnly)}
      </Modal>
    );
  }

  return (
    <Modal show onHide={onClose} centered size="lg">
      <Form noValidate onSubmit={handleSubmit(onSubmit)}>
        <Modal.Header closeButton>
          <Modal.Title as="h5">
            {model
              ? t('adminLlm.models.editTitle', { name: model.name })
              : t('adminLlm.models.addTitle')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {errorAlert}
          <Row className="g-3">
            <Col md={6}>
              <Form.Group controlId="llm-model-name">
                <Form.Label>{t('adminLlm.models.fields.name')}</Form.Label>
                <Form.Control isInvalid={!!errors.name} {...register('name')} />
                <Form.Control.Feedback type="invalid">
                  {errors.name?.message}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>
            <Col md={6}>
              <Form.Group controlId="llm-model-provider">
                <Form.Label>{t('adminLlm.models.fields.provider')}</Form.Label>
                <Form.Select {...register('provider')}>
                  {LLM_PROVIDERS.map(p => (
                    <option key={p} value={p}>
                      {t(`adminLlm.providers.${p}`)}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>
            </Col>
            <Col md={6}>
              <Form.Group controlId="llm-model-credential-kind">
                <Form.Label>
                  {t('adminLlm.models.fields.credentialKind')}
                </Form.Label>
                <Form.Select {...register('credentialKind')}>
                  <option value="org">
                    {t('adminLlm.credentialKind.org')}
                  </option>
                  <option value="user_account" disabled={provider !== 'openai'}>
                    {t('adminLlm.credentialKind.user_account')}
                  </option>
                </Form.Select>
              </Form.Group>
            </Col>
            <Col md={6}>
              {!isUserAccount && (
                <Form.Group controlId="llm-model-credential">
                  <Form.Label>
                    {t('adminLlm.models.fields.credential')}
                  </Form.Label>
                  <Form.Select
                    isInvalid={!!errors.credentialUuid}
                    {...register('credentialUuid')}
                  >
                    <option value="">
                      {t('adminLlm.models.fields.credentialPlaceholder')}
                    </option>
                    {credentialOptions.map(c => (
                      <option key={c.uuid} value={c.uuid}>
                        {c.status === 'active'
                          ? c.name
                          : t('adminLlm.models.fields.credentialDisabled', {
                              name: c.name
                            })}
                      </option>
                    ))}
                  </Form.Select>
                  <Form.Control.Feedback type="invalid">
                    {errors.credentialUuid?.message}
                  </Form.Control.Feedback>
                </Form.Group>
              )}
            </Col>
            <Col md={12}>
              <Form.Group controlId="llm-model-id">
                <Form.Label>{t('adminLlm.models.fields.modelId')}</Form.Label>
                <Form.Control
                  className="font-monospace"
                  placeholder={t('adminLlm.models.fields.modelIdPlaceholder')}
                  isInvalid={!!errors.modelId}
                  {...register('modelId')}
                />
                <Form.Control.Feedback type="invalid">
                  {errors.modelId?.message}
                </Form.Control.Feedback>
                <Form.Text className="text-600">
                  {t('adminLlm.models.fields.modelIdHint')}
                </Form.Text>
              </Form.Group>
            </Col>
            <Col md={12}>
              <Form.Label as="p" className="mb-2">
                {t('adminLlm.models.fields.capabilities')}
              </Form.Label>
              <div className="d-flex flex-wrap gap-3">
                <Form.Check
                  type="switch"
                  id="llm-cap-chat"
                  label={t('adminLlm.capabilities.chat')}
                  {...register('chat')}
                />
                <Form.Check
                  type="switch"
                  id="llm-cap-streaming"
                  label={t('adminLlm.capabilities.streaming')}
                  {...register('streaming')}
                />
                <Form.Check
                  type="switch"
                  id="llm-cap-tools"
                  label={t('adminLlm.capabilities.tools')}
                  {...register('tools')}
                />
                <Form.Check
                  type="switch"
                  id="llm-cap-structured"
                  label={t('adminLlm.capabilities.structuredOutput')}
                  {...register('structuredOutput')}
                />
                {isUserAccount ? (
                  <Form.Check
                    type="switch"
                    id="llm-cap-embeddings"
                    label={t('adminLlm.capabilities.embeddings')}
                    checked={false}
                    disabled
                    readOnly
                  />
                ) : (
                  <Form.Check
                    type="switch"
                    id="llm-cap-embeddings"
                    label={t('adminLlm.capabilities.embeddings')}
                    {...register('embeddings')}
                  />
                )}
              </div>
              {embeddings && !isUserAccount && (
                <Form.Group className="mt-2" controlId="llm-model-dimensions">
                  <Form.Label>
                    {t('adminLlm.models.fields.dimensions')}
                  </Form.Label>
                  <Form.Control
                    type="number"
                    isInvalid={!!errors.dimensions}
                    {...register('dimensions')}
                  />
                  <Form.Control.Feedback type="invalid">
                    {errors.dimensions?.message}
                  </Form.Control.Feedback>
                </Form.Group>
              )}
            </Col>
            <Col md={4}>
              <Form.Group controlId="llm-model-temperature">
                <Form.Label>
                  {t('adminLlm.models.fields.temperature')}
                </Form.Label>
                {isUserAccount ? (
                  <Form.Control type="number" value="" disabled readOnly />
                ) : (
                  <Form.Control
                    type="number"
                    step="0.1"
                    isInvalid={!!errors.temperature}
                    {...register('temperature')}
                  />
                )}
                <Form.Control.Feedback type="invalid">
                  {errors.temperature?.message}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>
            <Col md={4}>
              <Form.Group controlId="llm-model-max-tokens">
                <Form.Label>
                  {t('adminLlm.models.fields.maxOutputTokens')}
                </Form.Label>
                {isUserAccount ? (
                  <Form.Control type="number" value="" disabled readOnly />
                ) : (
                  <Form.Control
                    type="number"
                    isInvalid={!!errors.maxOutputTokens}
                    {...register('maxOutputTokens')}
                  />
                )}
                <Form.Control.Feedback type="invalid">
                  {errors.maxOutputTokens?.message}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>
            <Col md={4}>
              <Form.Group controlId="llm-model-effort">
                <Form.Label>{t('adminLlm.models.fields.effort')}</Form.Label>
                <Form.Select {...register('effort')}>
                  {EFFORTS.map(e => (
                    <option key={e || 'default'} value={e}>
                      {t(`adminLlm.effort.${e || 'default'}`)}
                    </option>
                  ))}
                </Form.Select>
              </Form.Group>
            </Col>
            <Col md={12}>
              <Form.Label as="p" className="mb-2">
                {t('adminLlm.models.fields.purposes')}
              </Form.Label>
              {fields.map((f, i) => (
                <Row key={f.id} className="g-2 mb-2 align-items-start">
                  <Col xs={7}>
                    <Form.Control
                      aria-label={t('adminLlm.models.fields.purposeLabel', {
                        index: i + 1
                      })}
                      placeholder={t(
                        'adminLlm.models.fields.purposePlaceholder'
                      )}
                      isInvalid={!!errors.purposes?.[i]?.purpose}
                      {...register(`purposes.${i}.purpose` as const)}
                    />
                    <Form.Control.Feedback type="invalid">
                      {errors.purposes?.[i]?.purpose?.message}
                    </Form.Control.Feedback>
                  </Col>
                  <Col xs={3}>
                    <Form.Control
                      type="number"
                      aria-label={t('adminLlm.models.fields.priorityLabel', {
                        index: i + 1
                      })}
                      isInvalid={!!errors.purposes?.[i]?.priority}
                      {...register(`purposes.${i}.priority` as const)}
                    />
                    <Form.Control.Feedback type="invalid">
                      {errors.purposes?.[i]?.priority?.message}
                    </Form.Control.Feedback>
                  </Col>
                  <Col xs={2}>
                    <IconButton
                      variant="orkestra-default"
                      size="sm"
                      icon={faMinus}
                      title={t('adminLlm.models.fields.removePurpose')}
                      aria-label={t('adminLlm.models.fields.removePurpose')}
                      disabled={fields.length === 1}
                      onClick={() => remove(i)}
                    />
                  </Col>
                </Row>
              ))}
              <IconButton
                variant="orkestra-default"
                size="sm"
                icon={faPlus}
                disabled={fields.length >= 16}
                onClick={() => append({ purpose: '', priority: 10 })}
              >
                {t('adminLlm.models.fields.addPurpose')}
              </IconButton>
              <Form.Text className="d-block text-600">
                {t('adminLlm.models.fields.purposesHint')}
              </Form.Text>
            </Col>
            <Col md={12}>{accessSelect}</Col>
            {access === 'granted' && <Col md={12}>{grantsPicker}</Col>}
            {isUserAccount && (
              <Col md={12}>
                <Form.Text className="text-600">
                  {t('adminLlm.models.userAccountHint')}
                </Form.Text>
              </Col>
            )}
          </Row>
        </Modal.Body>
        {footer()}
      </Form>
    </Modal>
  );
};

export default ModelEditorModal;
