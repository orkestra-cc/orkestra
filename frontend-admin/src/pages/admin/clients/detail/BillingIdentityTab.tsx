import { useEffect, useState } from 'react';
import { Alert, Button, Form, Row, Col, Spinner } from 'react-bootstrap';
import { toast } from 'react-toastify';
import SubtleBadge from 'components/common/SubtleBadge';
import type {
  Org,
  SetBillingIdentityInput,
  TenantAddress,
  FatturaPAProfile,
} from 'store/api/tenantApi';
import {
  useSetTenantBillingIdentityAdminMutation,
  useSetTenantItalianBillableAdminMutation,
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
  country: '',
};

const EMPTY_FATTURAPA: Required<FatturaPAProfile> = {
  codiceDestinatario: '',
  pecDestinatario: '',
  isPA: false,
  codiceUfficio: '',
  riferimentoAmm: '',
  convenzioneNumero: '',
};

function orgToForm(org: Org): FormState {
  return {
    isCompany: !!org.isCompany,
    legalName: org.legalName ?? '',
    vatNumber: org.vatNumber ?? '',
    fiscalCode: org.fiscalCode ?? '',
    billingAddress: { ...EMPTY_ADDRESS, ...(org.billingAddress ?? {}) },
    fatturaPA: { ...EMPTY_FATTURAPA, ...(org.fatturaPA ?? {}) },
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
    convenzioneNumero: emptyToUndefined(form.fatturaPA.convenzioneNumero),
  };
  const billingAddress: TenantAddress = {
    line1: emptyToUndefined(form.billingAddress.line1),
    line2: emptyToUndefined(form.billingAddress.line2),
    city: emptyToUndefined(form.billingAddress.city),
    province: emptyToUndefined(form.billingAddress.province),
    postalCode: emptyToUndefined(form.billingAddress.postalCode),
    country: emptyToUndefined(form.billingAddress.country),
  };
  return {
    isCompany: form.isCompany,
    legalName: form.legalName,
    vatNumber: form.vatNumber,
    fiscalCode: form.fiscalCode,
    billingAddress,
    fatturaPA,
  };
}

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || 'unknown error';
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

  const onSave = async () => {
    try {
      await patch({ tenantId: org.id, body: buildPatch(form) }).unwrap();
      toast.success('Billing identity saved');
    } catch (err) {
      toast.error('Save failed: ' + extractError(err));
    }
  };

  const onToggleBillable = async (next: boolean) => {
    try {
      await toggleItalianBillable({ tenantId: org.id, enabled: next }).unwrap();
      toast.success(
        next
          ? 'Italian billable mode enabled'
          : 'Italian billable mode disabled',
      );
    } catch (err) {
      toast.error('Toggle failed: ' + extractError(err));
    }
  };

  return (
    <Form>
      <Alert variant="info" className="fs-10 py-2">
        These fields populate the FatturaPA recipient party at invoice send
        time. Either <code>CodiceDestinatario</code> (7-char SDI code) or{' '}
        <code>PECDestinatario</code> is required to enable Italian billable
        mode.
      </Alert>

      <div className="d-flex justify-content-between align-items-center mb-3">
        <div>
          <span className="fw-semibold me-2">Italian billable</span>
          {org.isItalianBillable ? (
            <SubtleBadge bg="success" pill>
              enabled
            </SubtleBadge>
          ) : (
            <SubtleBadge bg="secondary" pill>
              disabled
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
            ? 'Saving…'
            : org.isItalianBillable
              ? 'Disable'
              : 'Enable'}
        </Button>
      </div>
      {!org.isItalianBillable && !hasRouting && (
        <Form.Text className="text-muted d-block mb-3">
          Add a <code>CodiceDestinatario</code> or <code>PECDestinatario</code>{' '}
          and save before enabling.
        </Form.Text>
      )}

      <Row>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Legal entity</Form.Label>
            <Form.Check
              type="switch"
              id="isCompany"
              label={form.isCompany ? 'Company' : 'Natural person / sole proprietor'}
              checked={form.isCompany}
              onChange={(e) =>
                setForm((s) => ({ ...s, isCompany: e.target.checked }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Legal name</Form.Label>
            <Form.Control
              value={form.legalName}
              onChange={(e) =>
                setForm((s) => ({ ...s, legalName: e.target.value }))
              }
              placeholder={form.isCompany ? 'Acme S.r.l.' : 'Mario Rossi'}
            />
          </Form.Group>
        </Col>
      </Row>

      <Row>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">VAT number</Form.Label>
            <Form.Control
              value={form.vatNumber}
              onChange={(e) =>
                setForm((s) => ({ ...s, vatNumber: e.target.value }))
              }
              placeholder="IT12345678901"
            />
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Fiscal code</Form.Label>
            <Form.Control
              value={form.fiscalCode}
              onChange={(e) =>
                setForm((s) => ({ ...s, fiscalCode: e.target.value }))
              }
              placeholder="RSSMRA80A01H501T"
            />
          </Form.Group>
        </Col>
      </Row>

      <h6 className="fw-semibold fs-9 mt-4">Billing address</h6>
      <Row>
        <Col md={8}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Address line 1</Form.Label>
            <Form.Control
              value={form.billingAddress.line1}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  billingAddress: { ...s.billingAddress, line1: e.target.value },
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={4}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Address line 2</Form.Label>
            <Form.Control
              value={form.billingAddress.line2}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  billingAddress: { ...s.billingAddress, line2: e.target.value },
                }))
              }
            />
          </Form.Group>
        </Col>
      </Row>
      <Row>
        <Col md={4}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">City</Form.Label>
            <Form.Control
              value={form.billingAddress.city}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  billingAddress: { ...s.billingAddress, city: e.target.value },
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={2}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Province</Form.Label>
            <Form.Control
              value={form.billingAddress.province}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  billingAddress: {
                    ...s.billingAddress,
                    province: e.target.value,
                  },
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={3}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Postal code</Form.Label>
            <Form.Control
              value={form.billingAddress.postalCode}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  billingAddress: {
                    ...s.billingAddress,
                    postalCode: e.target.value,
                  },
                }))
              }
            />
          </Form.Group>
        </Col>
        <Col md={3}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">Country</Form.Label>
            <Form.Control
              value={form.billingAddress.country}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  billingAddress: {
                    ...s.billingAddress,
                    country: e.target.value,
                  },
                }))
              }
              placeholder="IT"
              maxLength={2}
            />
          </Form.Group>
        </Col>
      </Row>

      <h6 className="fw-semibold fs-9 mt-4">FatturaPA routing</h6>
      <Row>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">
              Codice Destinatario (SDI)
            </Form.Label>
            <Form.Control
              className="font-monospace"
              value={form.fatturaPA.codiceDestinatario}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  fatturaPA: {
                    ...s.fatturaPA,
                    codiceDestinatario: e.target.value,
                  },
                }))
              }
              placeholder="ABC1234"
              maxLength={7}
            />
          </Form.Group>
        </Col>
        <Col md={6}>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold fs-10">PEC Destinatario</Form.Label>
            <Form.Control
              type="email"
              value={form.fatturaPA.pecDestinatario}
              onChange={(e) =>
                setForm((s) => ({
                  ...s,
                  fatturaPA: { ...s.fatturaPA, pecDestinatario: e.target.value },
                }))
              }
              placeholder="fatture@pec.example.it"
            />
          </Form.Group>
        </Col>
      </Row>

      <Form.Group className="mb-3">
        <Form.Check
          type="switch"
          id="isPA"
          label="Public administration (FatturaPA-PA)"
          checked={form.fatturaPA.isPA}
          onChange={(e) =>
            setForm((s) => ({
              ...s,
              fatturaPA: { ...s.fatturaPA, isPA: e.target.checked },
            }))
          }
        />
      </Form.Group>

      {form.fatturaPA.isPA && (
        <Row>
          <Col md={4}>
            <Form.Group className="mb-3">
              <Form.Label className="fw-semibold fs-10">
                Codice Ufficio
              </Form.Label>
              <Form.Control
                value={form.fatturaPA.codiceUfficio}
                onChange={(e) =>
                  setForm((s) => ({
                    ...s,
                    fatturaPA: { ...s.fatturaPA, codiceUfficio: e.target.value },
                  }))
                }
              />
            </Form.Group>
          </Col>
          <Col md={4}>
            <Form.Group className="mb-3">
              <Form.Label className="fw-semibold fs-10">
                Riferimento Amministrativo
              </Form.Label>
              <Form.Control
                value={form.fatturaPA.riferimentoAmm}
                onChange={(e) =>
                  setForm((s) => ({
                    ...s,
                    fatturaPA: {
                      ...s.fatturaPA,
                      riferimentoAmm: e.target.value,
                    },
                  }))
                }
              />
            </Form.Group>
          </Col>
          <Col md={4}>
            <Form.Group className="mb-3">
              <Form.Label className="fw-semibold fs-10">
                Convenzione N°
              </Form.Label>
              <Form.Control
                value={form.fatturaPA.convenzioneNumero}
                onChange={(e) =>
                  setForm((s) => ({
                    ...s,
                    fatturaPA: {
                      ...s.fatturaPA,
                      convenzioneNumero: e.target.value,
                    },
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
              Saving…
            </>
          ) : (
            'Save billing identity'
          )}
        </Button>
      </div>
    </Form>
  );
};

export default BillingIdentityTab;
