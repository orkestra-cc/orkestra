import { useMemo, useState } from 'react';
import { Alert, Button, Col, Form, Row, Spinner } from 'react-bootstrap';
import { useForm } from 'react-hook-form';
import { yupResolver } from '@hookform/resolvers/yup';
import * as yup from 'yup';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import {
  passwordUiVisible,
  useChangePasswordMutation,
  useGetPasswordEnrollmentPolicyQuery,
  useGetCurrentUserQuery,
  useGetSelfAuthMethodsQuery,
  useSetInitialPasswordMutation
} from 'store/api/authApi';

// The schema depends on the live password policy, so it is built per
// `minLength` rather than declared as a module constant.
const makeSchema = (minLength: number, hasPassword: boolean, t: TFunction) =>
  yup.object({
    oldPassword: hasPassword
      ? yup
          .string()
          .required(t('userSecurity.passwordTab.errorRequiredCurrent'))
      : yup.string().defined().default(''),
    newPassword: yup
      .string()
      .required(t('userSecurity.passwordTab.errorRequiredNew'))
      .min(
        minLength,
        t('userSecurity.passwordTab.errorTooShort', { count: minLength })
      ),
    confirmPassword: yup
      .string()
      .required(t('userSecurity.passwordTab.errorRequiredConfirm'))
      .oneOf(
        [yup.ref('newPassword')],
        t('userSecurity.passwordTab.errorMismatch')
      )
  });

type PasswordForm = yup.InferType<ReturnType<typeof makeSchema>>;

// The pane carries no card of its own: the tab strip above already names
// this section (same rule the /admin/compliance panes follow).
const PasswordTab = () => {
  const { t } = useTranslation();
  const policyQuery = useGetPasswordEnrollmentPolicyQuery();
  const methodsQuery = useGetSelfAuthMethodsQuery();
  const userQuery = useGetCurrentUserQuery();
  const [changePassword, changeState] = useChangePasswordMutation();
  const [setInitialPassword, enrollmentState] = useSetInitialPasswordMutation();
  const [alreadySet, setAlreadySet] = useState(false);
  const isLoading = changeState.isLoading || enrollmentState.isLoading;
  const policy = policyQuery.data;
  const authMethods = methodsQuery.data;

  const minLength = policy?.passwordMinLength ?? 10;
  const hasPassword = authMethods?.hasPasswordSet === true;
  const passwordKeptButUnusable =
    authMethods?.hasPasswordSet &&
    authMethods?.passwordUsableForLogin === false;

  const schema = useMemo(
    () => makeSchema(minLength, hasPassword, t),
    [minLength, hasPassword, t]
  );

  const {
    register,
    handleSubmit,
    reset,
    setError,
    clearErrors,
    formState: { errors }
  } = useForm<PasswordForm>({
    resolver: yupResolver(schema),
    defaultValues: { oldPassword: '', newPassword: '', confirmPassword: '' }
  });

  const onSubmit = async (values: PasswordForm) => {
    clearErrors('root');
    setAlreadySet(false);
    try {
      if (hasPassword) {
        await changePassword({
          currentPassword: values.oldPassword,
          newPassword: values.newPassword
        }).unwrap();
        toast.success(t('userSecurity.passwordTab.successToast'));
      } else {
        await setInitialPassword({ newPassword: values.newPassword }).unwrap();
        toast.success(t('userSecurity.passwordTab.enrollmentSuccessToast'));
      }
      reset();
    } catch (err: unknown) {
      const error = err as {
        status?: number;
        data?: { code?: string; detail?: string; title?: string };
      };
      const data = error?.data;
      if (
        data?.code === 'step_up_required' ||
        data?.code === 'password_confirm_required' ||
        data?.code === 'reauthentication_required'
      )
        return; // The shared API layer owns proof prompts and redirects.
      if (
        !hasPassword &&
        error.status === 409 &&
        data?.code === 'auth.password_already_set'
      ) {
        reset();
        setAlreadySet(true);
        void methodsQuery.refetch();
        return;
      }
      setError('root', {
        message:
          data?.detail ||
          data?.title ||
          t('userSecurity.passwordTab.errorGeneric')
      });
    }
  };

  if (policyQuery.isError || methodsQuery.isError || userQuery.isError) {
    return (
      <Alert variant="danger" className="fs-10">
        {t('userSecurity.passwordTab.loadError')}
      </Alert>
    );
  }
  if (
    policyQuery.isFetching ||
    methodsQuery.isFetching ||
    userQuery.isFetching
  ) {
    return (
      <div role="status" className="fs-10 text-muted">
        <Spinner animation="border" size="sm" className="me-2" />
        {t('userSecurity.passwordTab.loading')}
      </div>
    );
  }
  if (!policy || !authMethods || !userQuery.data) {
    return (
      <Alert variant="danger" className="fs-10">
        {t('userSecurity.passwordTab.loadError')}
      </Alert>
    );
  }
  if (!hasPassword && !passwordUiVisible(policy)) {
    return (
      <Alert variant="info" className="fs-10">
        {t('userSecurity.passwordTab.enrollmentDisabled')}
      </Alert>
    );
  }

  return (
    <Row>
      {/* A password field has no reason to be 1000px wide — the console's
          forms sit in a constrained column so the label/field pairing stays
          scannable at operator widths. */}
      <Col lg={7} xxl={6}>
        {passwordKeptButUnusable && (
          <Alert variant="info" className="fs-10">
            {t('userSecurity.passwordTab.keptNotice')}
          </Alert>
        )}
        {alreadySet && (
          <Alert variant="info" className="fs-10">
            {t('userSecurity.passwordTab.alreadySetNotice')}
          </Alert>
        )}
        {errors.root && (
          <Alert variant="danger" className="fs-10">
            {errors.root.message}
          </Alert>
        )}
        {!hasPassword && (
          <h5>{t('userSecurity.passwordTab.enrollmentHeading')}</h5>
        )}
        <p className="fs-10 text-muted mb-3">
          {t(
            hasPassword
              ? 'userSecurity.passwordTab.intro'
              : 'userSecurity.passwordTab.enrollmentIntro'
          )}
        </p>
        <Form onSubmit={handleSubmit(onSubmit)} noValidate>
          {!hasPassword && (
            <Form.Group className="mb-3" controlId="self-account-email">
              <Form.Label>
                {t('userSecurity.passwordTab.labelEmail')}
              </Form.Label>
              <Form.Control
                type="email"
                value={userQuery.data.email}
                readOnly
              />
              <Form.Text className="text-muted">
                {t('userSecurity.passwordTab.emailHelp')}
              </Form.Text>
            </Form.Group>
          )}
          {hasPassword && (
            <Form.Group className="mb-3" controlId="self-old-password">
              <Form.Label>
                {t('userSecurity.passwordTab.labelCurrent')}
              </Form.Label>
              <Form.Control
                type="password"
                autoComplete="current-password"
                required={hasPassword}
                disabled={isLoading}
                isInvalid={!!errors.oldPassword}
                {...register('oldPassword')}
              />
              <Form.Control.Feedback type="invalid">
                {errors.oldPassword?.message}
              </Form.Control.Feedback>
            </Form.Group>
          )}
          <Form.Group className="mb-3" controlId="self-new-password">
            <Form.Label>{t('userSecurity.passwordTab.labelNew')}</Form.Label>
            <Form.Control
              type="password"
              autoComplete="new-password"
              required
              disabled={isLoading}
              isInvalid={!!errors.newPassword}
              {...register('newPassword')}
            />
            <Form.Control.Feedback type="invalid">
              {errors.newPassword?.message}
            </Form.Control.Feedback>
            <Form.Text className="text-muted">
              {t('userSecurity.passwordTab.minLengthHelp', {
                count: minLength
              })}
            </Form.Text>
          </Form.Group>
          <Form.Group className="mb-4" controlId="self-confirm-password">
            <Form.Label>
              {t('userSecurity.passwordTab.labelConfirm')}
            </Form.Label>
            <Form.Control
              type="password"
              autoComplete="new-password"
              required
              disabled={isLoading}
              isInvalid={!!errors.confirmPassword}
              {...register('confirmPassword')}
            />
            <Form.Control.Feedback type="invalid">
              {errors.confirmPassword?.message}
            </Form.Control.Feedback>
          </Form.Group>
          {/* The one primary action of this pane — solid Orkestra Blue. */}
          <Button type="submit" variant="orkestra-primary" disabled={isLoading}>
            {isLoading ? (
              <>
                <Spinner animation="border" size="sm" className="me-2" />
                {t(
                  hasPassword
                    ? 'userSecurity.passwordTab.submitting'
                    : 'userSecurity.passwordTab.enrollmentSubmitting'
                )}
              </>
            ) : (
              t(
                hasPassword
                  ? 'userSecurity.passwordTab.submit'
                  : 'userSecurity.passwordTab.enrollmentSubmit'
              )
            )}
          </Button>
        </Form>
      </Col>
    </Row>
  );
};

export default PasswordTab;
