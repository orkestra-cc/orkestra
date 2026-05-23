import { useEffect, useState } from 'react';
import { Alert, Button, Form, Row, Col, Spinner } from 'react-bootstrap';
import { Trans, useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import type {
  Org,
  SetBillingIdentityInput,
  TenantAddress,
  FatturaPAProfile
} from 'store/api/tenantApi';
import {
  useSetTenantBillingIdentityAdminMutation,
  useSetTenantItalianBillableAdminMutation
} from 'store/api/tenantApi';

interface Props {
  org: Org;
}

interface FormState {
  isCompany: boolean;
  legalName: string;
  vatNumber: string;
  fiscalCode: string;
  billingAddress: Required<TenantAddress>;
  fatturaPA: Required<FatturaPAProfile>;
}

const EMPTY_ADDRESS: Required<TenantAddress> = {
  line1: '',
  line2: '',
  city: '',
  province: '',
  postalCode: '',
  country: ''
};

const EMPTY_FATTURAPA: Required<FatturaPAProfile> = {
  codiceDestinatario: '',
  pecDestinatario: '',
  isPA: false,
  codiceUfficio: '',
  riferimentoAmm: '',
  convenzioneNumero: ''
};

function orgToForm(org: Org): FormState {
  return {
    isCompany: !!org.isCompany,
    legalName: org.legalName ?? '',
    vatNumber: org.vatNumber ?? '',
    fiscalCode: org.fiscalCode ?? '',
    billingAddress: { ...EMPTY_ADDRESS, ...(org.billingAddress ?? {}) },
    fatturaPA: { ...EMPTY_FATTURAPA, ...(org.fatturaPA ?? {}) }
  };
}

function emptyToUndefined(value: string): string | undefined {
  const t = value.trim();
  return t === '' ? undefined : t;
}

function buildPatch(form: FormState): SetBillingIdentityInput {
  const fatturaPA: FatturaPAProfile = {
    codiceDestinatario: emptyToUndefined(form.fatturaPA.codiceDestinatario),
    pecDestinatario: emptyToUndefined(form.fatturaPA.pecDestinatario),
    isPA: form.fatturaPA.isPA,
    codiceUfficio: emptyToUndefined(form.fatturaPA.codiceUfficio),
    riferimentoAmm: emptyToUndefined(form.fatturaPA.riferimentoAmm),
    convenzioneNumero: emptyToUndefined(form.fatturaPA.convenzioneNumero)
  };
  const billingAddress: TenantAddress = {
    line1: emptyToUndefined(form.billingAddress.line1),
    line2: emptyToUndefined(form.billingAddress.line2),
    city: emptyToUndefined(form.billingAddress.city),
    province: emptyToUndefined(form.billingAddress.province),
    postalCode: emptyToUndefined(form.billingAddress.postalCode),
    country: emptyToUndefined(form.billingAddress.country)
  };
  return {
    isCompany: form.isCompany,
    legalName: form.legalName,
    vatNumber: form.vatNumber,
    fiscalCode: form.fiscalCode,
    billingAddress,
    fatturaPA
  };
}

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

/**
 * Billing identity tab — Phase 1 of the Unified Client Aggregate. Edits
 * the tenant's billing-identity sub-document (legal entity discriminator,
 * VAT/fiscal codes, billing address, FatturaPA routing) via the admin
 * surface. The Italian-billable toggle has its own endpoint so the FatturaPA
 * routing precondition can be enforced server-side.
 */
const BillingIdentityTab: React.FC<Props> = ({ org }) => {
  const { t } = useTranslation();
  const [form, setForm] = useState<FormState>(() => orgToForm(org));
  const [patch, { isLoading: isSaving }] =
    useSetTenantBillingIdentityAdminMutation();
  const [toggleItalianBillable, { isLoading: isToggling }] =
    useSetTenantItalianBillableAdminMutation();

  // Re-seed when the underlying org row changes (e.g. after a save invalidates
  // the cache and refetches). The optimistic-edit case keeps user input.
  useEffect(() => {
    setForm(orgToForm(org));
  }, [org]);

  const hasRouting =
    !!form.fatturaPA.codiceDestinatario.trim() ||
    !!form.fatturaPA.pecDestinatario.trim();

  const unknownErr = t('adminClients.billingIdentity.errorUnknown');

  const onSave = async () => {
    try {
      await patch({ tenantId: org.id, body: buildPatch(form) }).unwrap();
      toast.success(t('adminClients.billingIdentity.toastSaved'));
    } catch (err) {
      toast.error(
        t('adminClients.billingIdentity.toastSaveFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  const onToggleBillable = async (next: boolean) => {
    try {
      await toggleItalianBillable({ tenantId: org.id, enabled: next }).unwrap();
      toast.success(
        next
          ? t('adminClients.billingIdentity.toastBillableEnabled')
          : t('adminClients.billingIdentity.toastBillableDisabled')
      );
    } catch (err) {
      toast.error(
        t('adminClients.billingIdentity.toastToggleFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  return (
    <Form>
      <Alert variant="info" className="fs-10 py-2">
        <Trans
          i18nKey="adminClients.billingIdentity.intro"
          components={{ code: <code /> }}
        />
      </Alert>

      <div className="d-flex justify-content-between align-items-center mb-3">
        <div>
          <span className="fw-semibold me-2">
            {t('adminClients.billingIdentity.italianBillable')}
          </span>
          {org.isItalianBillable ? (
            <SubtleBadge bg="success" pill>
              {t('adminClients.billingIdentity.enabledBadge')}
            </SubtleBadge>
          ) : (
            <SubtleBadge bg="secondary" pill>
              {t('adminClients.billingIdentity.disabledBadge')}
            </SubtleBadge>
          )}
        </div>
        <Button
          variant={org.isItalianBillable ? 'outline-secondary' : 'primary'}
          size="sm"
          disabled={isToggling || (!org.isItalianBillable && !hasRouting)}
          onClick={() => onToggleBillable(!org.isItalianBillable)}
        >
          {isToggling
            ? t('adminClients.billingIdentity.saving')
            : org.isItalianBillable
              ? t('adminClients.billingIdentity.disable')
              : t('adminClients.billingIdentity.enable')}
        </Button>
      </div>
      {!org.isItalianBillable && !hasRouting && (
        <Form.Text className="text-muted d-block mb-3">
          <Trans
            i18nKey="adminClients.billingIdentity.routingMissing"
            components={{ code: <code /> }}
          />
        </Form.Text>
      )}

      <Row>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelLegalEntity')}
            </Form.Label>
            <Form.Check
              type="switch"
              id="isCompany"
              label={
                form.isCompany
                  ? t('adminClients.billingIdentity.switchCompany')
                  : t('adminClients.billingIdentity.switchPerson')
              }
              checked={form.isCompany}
              onChange={e =>
                setForm(s => ({ ...s, isCompany: e.target.checked }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelLegalName')}
            </Form.Label>
            <Form.Control
              value={form.legalName}
              onChange={e =>
                setForm(s => ({ ...s, legalName: e.target.value }))
              }
              placeholder={
                form.isCompany
                  ? t(
                      'adminClients.billingIdentity.placeholderLegalNameCompany'
                    )
                  : t('adminClients.billingIdentity.placeholderLegalNamePerson')
              }
            />
          </Form.Group>
        </Col>
      </Row>

      <Row>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelVatNumber')}
            </Form.Label>
            <Form.Control
              value={form.vatNumber}
              onChange={e =>
                setForm(s => ({ ...s, vatNumber: e.target.value }))
              }
              placeholder={t('adminClients.billingIdentity.placeholderVat')}
            />
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelFiscalCode')}
            </Form.Label>
            <Form.Control
              value={form.fiscalCode}
              onChange={e =>
                setForm(s => ({ ...s, fiscalCode: e.target.value }))
              }
              placeholder={t(
                'adminClients.billingIdentity.placeholderFiscalCode'
              )}
            />
          </Form.Group>
        </Col>
      </Row>

      <h6 className="fw-semibold fs-9 mt-4">
        {t('adminClients.billingIdentity.addressHeading')}
      </h6>
      <Row>
        <Col md={8}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelLine1')}
            </Form.Label>
            <Form.Control
              value={form.billingAddress.line1}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  billingAddress: { ...s.billingAddress, line1: e.target.value }
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={4}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelLine2')}
            </Form.Label>
            <Form.Control
              value={form.billingAddress.line2}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  billingAddress: { ...s.billingAddress, line2: e.target.value }
                }))
              }
            />
          </Form.Group>
        </Col>
      </Row>
      <Row>
        <Col md={4}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelCity')}
            </Form.Label>
            <Form.Control
              value={form.billingAddress.city}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  billingAddress: { ...s.billingAddress, city: e.target.value }
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={2}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelProvince')}
            </Form.Label>
            <Form.Control
              value={form.billingAddress.province}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  billingAddress: {
                    ...s.billingAddress,
                    province: e.target.value
                  }
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={3}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelPostalCode')}
            </Form.Label>
            <Form.Control
              value={form.billingAddress.postalCode}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  billingAddress: {
                    ...s.billingAddress,
                    postalCode: e.target.value
                  }
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={3}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelCountry')}
            </Form.Label>
            <Form.Control
              value={form.billingAddress.country}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  billingAddress: {
                    ...s.billingAddress,
                    country: e.target.value
                  }
                }))
              }
              placeholder={t('adminClients.billingIdentity.placeholderCountry')}
              maxLength={2}
            />
          </Form.Group>
        </Col>
      </Row>

      <h6 className="fw-semibold fs-9 mt-4">
        {t('adminClients.billingIdentity.fatturaPaHeading')}
      </h6>
      <Row>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelCodiceDestinatario')}
            </Form.Label>
            <Form.Control
              className="font-monospace"
              value={form.fatturaPA.codiceDestinatario}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  fatturaPA: {
                    ...s.fatturaPA,
                    codiceDestinatario: e.target.value
                  }
                }))
              }
              placeholder={t(
                'adminClients.billingIdentity.placeholderCodiceDestinatario'
              )}
              maxLength={7}
            />
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              {t('adminClients.billingIdentity.labelPecDestinatario')}
            </Form.Label>
            <Form.Control
              type="email"
              value={form.fatturaPA.pecDestinatario}
              onChange={e =>
                setForm(s => ({
                  ...s,
                  fatturaPA: { ...s.fatturaPA, pecDestinatario: e.target.value }
                }))
              }
              placeholder={t('adminClients.billingIdentity.placeholderPec')}
            />
          </Form.Group>
        </Col>
      </Row>

      <Form.Group className="mb-3">
        <Form.Check
          type="switch"
          id="isPA"
          label={t('adminClients.billingIdentity.switchIsPA')}
          checked={form.fatturaPA.isPA}
          onChange={e =>
            setForm(s => ({
              ...s,
              fatturaPA: { ...s.fatturaPA, isPA: e.target.checked }
            }))
          }
        />
      </Form.Group>

      {form.fatturaPA.isPA && (
        <Row>
          <Col md={4}>
            <Form.Group className="mb-3">
              <Form.Label className="fw-semibold fs-10">
                {t('adminClients.billingIdentity.labelCodiceUfficio')}
              </Form.Label>
              <Form.Control
                value={form.fatturaPA.codiceUfficio}
                onChange={e =>
                  setForm(s => ({
                    ...s,
                    fatturaPA: { ...s.fatturaPA, codiceUfficio: e.target.value }
                  }))
                }
              />
            </Form.Group>
          </Col>
          <Col md={4}>
            <Form.Group className="mb-3">
              <Form.Label className="fw-semibold fs-10">
                {t('adminClients.billingIdentity.labelRiferimentoAmm')}
              </Form.Label>
              <Form.Control
                value={form.fatturaPA.riferimentoAmm}
                onChange={e =>
                  setForm(s => ({
                    ...s,
                    fatturaPA: {
                      ...s.fatturaPA,
                      riferimentoAmm: e.target.value
                    }
                  }))
                }
              />
            </Form.Group>
          </Col>
          <Col md={4}>
            <Form.Group className="mb-3">
              <Form.Label className="fw-semibold fs-10">
                {t('adminClients.billingIdentity.labelConvenzioneNumero')}
              </Form.Label>
              <Form.Control
                value={form.fatturaPA.convenzioneNumero}
                onChange={e =>
                  setForm(s => ({
                    ...s,
                    fatturaPA: {
                      ...s.fatturaPA,
                      convenzioneNumero: e.target.value
                    }
                  }))
                }
              />
            </Form.Group>
          </Col>
        </Row>
      )}

      <div className="d-flex justify-content-end mt-3">
        <Button variant="primary" disabled={isSaving} onClick={onSave}>
          {isSaving ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" />
              {t('adminClients.billingIdentity.saving')}
            </>
          ) : (
            t('adminClients.billingIdentity.save')
          )}
        </Button>
      </div>
    </Form>
  );
};

export default BillingIdentityTab;
