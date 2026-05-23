import { useEffect, useState } from 'react';
import { Button, Form } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import type { Org } from 'store/api/tenantApi';
import { useUpdateOrgAdminMutation } from 'store/api/tenantApi';

interface Props {
  org: Org;
}

/**
 * Overview tab for a client (external tenant). Name and slug are editable;
 * the ID, owner, created/updated timestamps are read-only. Richer fields
 * (contact, VAT, Stripe customer ID) will surface in Phase 4 follow-up
 * once the update DTO accepts them — for now the admin can read them
 * on the Internal Tenants page's Overview (same component) and edit via
 * a dedicated form.
 */
const OverviewTab: React.FC<Props> = ({ org }) => {
  const { t } = useTranslation();
  const [updateOrg, { isLoading }] = useUpdateOrgAdminMutation();
  const [name, setName] = useState(org.name);
  const [slug, setSlug] = useState(org.slug);

  useEffect(() => {
    setName(org.name);
    setSlug(org.slug);
  }, [org.id, org.name, org.slug]);

  const dirty = name !== org.name || slug !== org.slug;

  const unknownErr = t('adminClients.overview.errorUnknown');

  const onSave = async () => {
    try {
      await updateOrg({
        tenantId: org.id,
        body: {
          name: name !== org.name ? name : undefined,
          slug: slug !== org.slug ? slug : undefined
        }
      }).unwrap();
      toast.success(t('adminClients.overview.toastSaved'));
    } catch (err: unknown) {
      toast.error(
        t('adminClients.overview.toastSaveFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  return (
    <Form className="px-1">
      <div className="row g-3">
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelName')}
          </Form.Label>
          <Form.Control value={name} onChange={e => setName(e.target.value)} />
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelSlug')}
          </Form.Label>
          <Form.Control value={slug} onChange={e => setSlug(e.target.value)} />
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelPlan')}
          </Form.Label>
          <Form.Control readOnly value={org.plan} className="fs-11" />
          <Form.Text muted>{t('adminClients.overview.planHelp')}</Form.Text>
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelStatus')}
          </Form.Label>
          <Form.Control readOnly value={org.status ?? '—'} className="fs-11" />
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelTenantId')}
          </Form.Label>
          <Form.Control
            readOnly
            value={org.id}
            className="fs-11 font-monospace"
          />
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelOwner')}
          </Form.Label>
          <Form.Control
            readOnly
            value={org.ownerUserUUID || '—'}
            className="fs-11 font-monospace"
          />
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelCreated')}
          </Form.Label>
          <Form.Control
            readOnly
            value={new Date(org.createdAt).toLocaleString()}
            className="fs-11"
          />
        </Form.Group>
        <Form.Group className="col-md-6">
          <Form.Label className="fw-semibold fs-10">
            {t('adminClients.overview.labelUpdated')}
          </Form.Label>
          <Form.Control
            readOnly
            value={new Date(org.updatedAt).toLocaleString()}
            className="fs-11"
          />
        </Form.Group>
      </div>
      <div className="d-flex justify-content-end mt-3">
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty || isLoading}
          onClick={onSave}
        >
          {isLoading
            ? t('adminClients.overview.saving')
            : t('adminClients.overview.save')}
        </Button>
      </div>
    </Form>
  );
};

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

export default OverviewTab;
