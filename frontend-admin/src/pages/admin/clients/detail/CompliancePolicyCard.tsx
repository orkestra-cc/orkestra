import { Fragment, useState } from 'react';
import { Alert, Button, Card, Spinner } from 'react-bootstrap';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import SubtleBadge from 'components/common/SubtleBadge';
import { useGetEffectivePolicyQuery } from 'store/api/complianceApi';
import AssignPolicyModal from './AssignPolicyModal';

// CompliancePolicyCard shows how the tenant's data is logged and retained:
// the effective policy, whether it is assigned or the platform default, the
// log-content modes and the audit retention by class.
const CompliancePolicyCard = ({ tenantId }: { tenantId: string }) => {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useGetEffectivePolicyQuery(tenantId);
  const [show, setShow] = useState(false);

  if (isLoading)
    return <Spinner animation="border" size="sm" className="mt-4" />;
  if (isError || !data) {
    return (
      <Alert variant="warning" className="fs-10 mt-4">
        {t('adminCompliance.tenantCard.loadError')}
      </Alert>
    );
  }
  const lc = data.policy.logContent;
  return (
    <Card className="shadow-none border mt-4">
      <Card.Header className="d-flex align-items-center bg-body-tertiary py-2">
        <h6 className="mb-0 me-auto">
          {t('adminCompliance.tenantCard.title')}
        </h6>
        <Button
          size="sm"
          variant="orkestra-default"
          onClick={() => setShow(true)}
        >
          {t('adminCompliance.tenantCard.change')}
        </Button>
      </Card.Header>
      <Card.Body className="fs-10">
        <div className="mb-3">
          <Link
            to={`/admin/compliance/policies/${encodeURIComponent(data.policy.uuid)}`}
            className="fw-semibold"
          >
            {data.policy.name}
          </Link>
          <SubtleBadge
            pill
            bg={data.source === 'assigned' ? 'info' : 'secondary'}
            className="ms-2"
          >
            {t(`adminCompliance.tenantCard.source.${data.source}`)}
          </SubtleBadge>
        </div>
        <dl className="row mb-0">
          <dt className="col-sm-5">{t('adminCompliance.fields.ipAddress')}</dt>
          <dd className="col-sm-7">
            {t(`adminCompliance.modes.ipAddress.${lc.ipAddress}`)}
          </dd>
          <dt className="col-sm-5">{t('adminCompliance.fields.userAgent')}</dt>
          <dd className="col-sm-7">
            {t(`adminCompliance.modes.userAgent.${lc.userAgent}`)}
          </dd>
          <dt className="col-sm-5">{t('adminCompliance.fields.subjectIds')}</dt>
          <dd className="col-sm-7">
            {t(`adminCompliance.modes.subjectIds.${lc.subjectIds}`)}
          </dd>
          <dt className="col-sm-12 mt-2">
            {t('adminCompliance.tenantCard.retention')}
          </dt>
          {data.retention.map(d => (
            <Fragment key={d.class}>
              <dt className="col-sm-5 fw-normal">
                {t(`adminCompliance.retentionClasses.${d.class}.label`)}
              </dt>
              <dd className="col-sm-7">
                {t('adminCompliance.tenantCard.days', { count: d.days })}
              </dd>
            </Fragment>
          ))}
        </dl>
      </Card.Body>
      <AssignPolicyModal
        show={show}
        tenantId={tenantId}
        currentPolicyId={data.source === 'assigned' ? data.policy.uuid : ''}
        onHide={() => setShow(false)}
      />
    </Card>
  );
};

export default CompliancePolicyCard;
