import { Col, Form, Row } from 'react-bootstrap';
import type { FieldError, UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import type {
  RetentionClassInfo,
  RetentionClassKey
} from 'store/api/complianceApi';
import {
  IP_MODES,
  ROLES,
  SUBJECT_MODES,
  UA_MODES,
  type PolicyFormValues
} from './policyForm';

interface Props {
  form: UseFormReturn<PolicyFormValues>;
  isPlatform: boolean;
  classes: RetentionClassInfo[];
  platformRetention: Partial<Record<RetentionClassKey, number>>;
  disabled: boolean;
}

// PolicySettingsForm edits log content, retention by class (with purpose,
// legal basis, minimum and default from the catalog) and accountability.
const PolicySettingsForm = ({
  form,
  isPlatform,
  classes,
  platformRetention,
  disabled
}: Props) => {
  const { t } = useTranslation();
  const {
    register,
    formState: { errors }
  } = form;
  // Server issues carry their own translated message; client (shape) errors
  // get a generic one.
  const errorText = (e?: FieldError) =>
    e
      ? e.type === 'server'
        ? e.message
        : t('adminCompliance.policyForm.invalid')
      : undefined;

  return (
    <fieldset disabled={disabled}>
      <Row className="g-3 mb-4">
        <Col md={6}>
          <Form.Group controlId="policy-name">
            <Form.Label>{t('adminCompliance.fields.name')}</Form.Label>
            <Form.Control isInvalid={!!errors.name} {...register('name')} />
            <Form.Control.Feedback type="invalid">
              {errorText(errors.name)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group controlId="policy-description">
            <Form.Label>{t('adminCompliance.fields.description')}</Form.Label>
            <Form.Control
              isInvalid={!!errors.description}
              {...register('description')}
            />
            <Form.Control.Feedback type="invalid">
              {errorText(errors.description)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
      </Row>

      <h6 className="text-700 mb-3">
        {t('adminCompliance.policyForm.sectionContent')}
      </h6>
      <Row className="g-3 mb-4">
        <Col md={4}>
          <Form.Group controlId="policy-ip">
            <Form.Label>{t('adminCompliance.fields.ipAddress')}</Form.Label>
            <Form.Select
              isInvalid={!!errors.ipAddress}
              {...register('ipAddress')}
            >
              {IP_MODES.map(m => (
                <option key={m} value={m}>
                  {t(`adminCompliance.modes.ipAddress.${m}`)}
                </option>
              ))}
            </Form.Select>
            <Form.Control.Feedback type="invalid">
              {errorText(errors.ipAddress)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col md={4}>
          <Form.Group controlId="policy-ua">
            <Form.Label>{t('adminCompliance.fields.userAgent')}</Form.Label>
            <Form.Select {...register('userAgent')}>
              {UA_MODES.map(m => (
                <option key={m} value={m}>
                  {t(`adminCompliance.modes.userAgent.${m}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col md={4}>
          <Form.Group controlId="policy-subjects">
            <Form.Label>{t('adminCompliance.fields.subjectIds')}</Form.Label>
            <Form.Select {...register('subjectIds')}>
              {SUBJECT_MODES.map(m => (
                <option key={m} value={m}>
                  {t(`adminCompliance.modes.subjectIds.${m}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col md={12}>
          <Form.Group controlId="policy-pii">
            <Form.Label>{t('adminCompliance.fields.piiKeys')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              isInvalid={!!errors.piiKeys}
              {...register('piiKeys')}
            />
            <Form.Text className="fs-11">
              {t('adminCompliance.policyForm.piiKeysHelp')}
            </Form.Text>
            <Form.Control.Feedback type="invalid">
              {errorText(errors.piiKeys)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col md={12}>
          <Form.Check
            type="switch"
            id="policy-scan"
            label={t('adminCompliance.policyForm.scanFreeTextLabel')}
            {...register('scanFreeText')}
          />
        </Col>
      </Row>

      <h6 className="text-700 mb-3">
        {t('adminCompliance.policyForm.sectionRetention')}
      </h6>
      <Row className="g-3 mb-4">
        {classes.map(c => {
          const editable = isPlatform || c.tenantSettable;
          const err = errors.retention?.[c.key];
          return (
            <Col md={6} lg={4} key={c.key}>
              <Form.Group controlId={`policy-retention-${c.key}`}>
                <Form.Label className="mb-0">
                  {t(`adminCompliance.retentionClasses.${c.key}.label`)}
                </Form.Label>
                <div className="fs-11 text-600 mb-1">
                  {t(`adminCompliance.retentionClasses.${c.key}.purpose`)} ·{' '}
                  {t(`adminCompliance.retentionClasses.${c.key}.legalBasis`)}
                </div>
                <Form.Control
                  type="number"
                  min={c.minDays}
                  max={3650}
                  disabled={!editable}
                  placeholder={
                    isPlatform
                      ? undefined
                      : t('adminCompliance.policyDetail.inherit', {
                          days: platformRetention[c.key] ?? c.defaultDays
                        })
                  }
                  isInvalid={!!err}
                  {...register(`retention.${c.key}` as const)}
                />
                <Form.Text className="fs-11">
                  {c.hardMinimum
                    ? `${t('adminCompliance.policyForm.hardMinimum')} · `
                    : ''}
                  {t('adminCompliance.policyForm.minDays', {
                    days: c.minDays
                  })}{' '}
                  ·{' '}
                  {t('adminCompliance.policyForm.defaultDays', {
                    days: c.defaultDays
                  })}
                  {editable
                    ? ''
                    : ` · ${t('adminCompliance.policyForm.platformOnly')}`}
                </Form.Text>
                <Form.Control.Feedback type="invalid">
                  {errorText(err)}
                </Form.Control.Feedback>
              </Form.Group>
            </Col>
          );
        })}
      </Row>

      <h6 className="text-700 mb-3">
        {t('adminCompliance.policyForm.sectionAccountability')}
      </h6>
      <Row className="g-3">
        <Col md={4}>
          <Form.Group controlId="policy-role">
            <Form.Label>{t('adminCompliance.fields.role')}</Form.Label>
            <Form.Select isInvalid={!!errors.role} {...register('role')}>
              {ROLES.map(r => (
                <option key={r} value={r}>
                  {t(`adminCompliance.roles.${r}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col md={4}>
          <Form.Group controlId="policy-ropa">
            <Form.Label>{t('adminCompliance.fields.ropaRef')}</Form.Label>
            <Form.Control
              isInvalid={!!errors.ropaRef}
              {...register('ropaRef')}
            />
            <Form.Control.Feedback type="invalid">
              {errorText(errors.ropaRef)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col md={4}>
          <Form.Group controlId="policy-assessment">
            <Form.Label>{t('adminCompliance.fields.assessmentRef')}</Form.Label>
            <Form.Control
              isInvalid={!!errors.assessmentRef}
              {...register('assessmentRef')}
            />
            <Form.Control.Feedback type="invalid">
              {errorText(errors.assessmentRef)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group controlId="policy-owner">
            <Form.Label>{t('adminCompliance.fields.owner')}</Form.Label>
            <Form.Control
              className="font-monospace"
              isInvalid={!!errors.owner}
              {...register('owner')}
            />
            <Form.Control.Feedback type="invalid">
              {errorText(errors.owner)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group controlId="policy-review">
            <Form.Label>{t('adminCompliance.fields.reviewDueAt')}</Form.Label>
            <Form.Control
              type="date"
              isInvalid={!!errors.reviewDueAt}
              {...register('reviewDueAt')}
            />
            <Form.Control.Feedback type="invalid">
              {errorText(errors.reviewDueAt)}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
      </Row>
    </fieldset>
  );
};

export default PolicySettingsForm;
