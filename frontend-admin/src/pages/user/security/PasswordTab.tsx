import { useMemo } from 'react';
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
  useGetAuthPolicyQuery
} from 'store/api/authApi';
import { useGetSelfAuthMethodsQuery } from 'store/api/authApi';

// The schema depends on the live password policy, so it is built per
// `minLength` rather than declared as a module constant.
const makeSchema = (minLength: number, t: TFunction) =>
  yup.object({
    oldPassword: yup.string().defined().default(''),
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

// PasswordTab implements the self-service password-change flow that
// the legacy /user/settings::ChangePassword card stubbed out. Wired
// to the existing /v1/auth/operator/change-password mutation; the
// backend enforces the current admin-managed password policy
// (min/max length, complexity, HIBP) — we display the minimum length
// up-front so the user knows what they're targeting.
//
// The pane carries no card of its own: the tab strip above already names
// this section (same rule the /admin/compliance panes follow).
const PasswordTab = () => {
  const { t } = useTranslation();
  const { data: policy } = useGetAuthPolicyQuery();
  const { data: authMethods } = useGetSelfAuthMethodsQuery();
  const [changePassword, { isLoading }] = useChangePasswordMutation();

  const minLength = policy?.passwordMinLength ?? 10;
  const hasPassword = authMethods?.hasPasswordSet ?? true;
  const passwordKeptButUnusable =
    authMethods?.hasPasswordSet &&
    authMethods?.passwordUsableForLogin === false;

  // Rebuilt when the policy query resolves — `minLength` is the only moving
  // part; the mismatch and required rules are static.
  const schema = useMemo(() => makeSchema(minLength, t), [minLength, t]);

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
    try {
      await changePassword({
        currentPassword: values.oldPassword,
        newPassword: values.newPassword
      }).unwrap();
      toast.success(t('userSecurity.passwordTab.successToast'));
      reset();
    } catch (err: unknown) {
      const data = (err as { data?: { detail?: string; title?: string } })
        ?.data;
      setError('root', {
        message:
          data?.detail ||
          data?.title ||
          t('userSecurity.passwordTab.errorGeneric')
      });
    }
  };

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
        {!hasPassword && passwordUiVisible(policy) && (
          <Alert variant="info" className="fs-10">
            {t('userSecurity.passwordTab.ssoOnlyHint')}
          </Alert>
        )}
        {errors.root && (
          <Alert variant="danger" className="fs-10">
            {errors.root.message}
          </Alert>
        )}
        <p className="fs-10 text-muted mb-3">
          {t('userSecurity.passwordTab.intro')}
        </p>
        <Form onSubmit={handleSubmit(onSubmit)} noValidate>
          <Form.Group className="mb-3" controlId="self-old-password">
            <Form.Label>
              {t('userSecurity.passwordTab.labelCurrent')}
            </Form.Label>
            <Form.Control
              type="password"
              autoComplete="current-password"
              required={hasPassword}
              disabled={isLoading}
              {...register('oldPassword')}
            />
          </Form.Group>
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
          <Button type="submit" variant="primary" disabled={isLoading}>
            {isLoading ? (
              <>
                <Spinner animation="border" size="sm" className="me-2" />
                {t('userSecurity.passwordTab.submitting')}
              </>
            ) : (
              t('userSecurity.passwordTab.submit')
            )}
          </Button>
        </Form>
      </Col>
    </Row>
  );
};

export default PasswordTab;
