// Custom-field schema editor — one schema per (tenant, target). The
// per-tenant configurability is the heart of design decision D05: the
// motor is generic, the data is shaped by the tenant.
//
// Phase 1 surface is a simple table-driven editor: add a field row,
// pick a type, supply enum options where applicable. Field-key edits
// trigger a Phase-2+ "schema migration warning" — not implemented
// here; the version counter lets a future surface diff schema changes.

import { useEffect, useState } from 'react';
import { Card, Nav, Tab, Form, Button, Table, Alert } from 'react-bootstrap';
import { useSearchParams } from 'react-router';
import { Trans, useTranslation } from 'react-i18next';
import {
  useGetCustomFieldSchemaQuery,
  useUpsertCustomFieldSchemaMutation,
  useDeleteCustomFieldSchemaMutation
} from 'store/api/marketingApi';
import type {
  CustomFieldTarget,
  FieldDef,
  CustomFieldType
} from 'types/marketing';

const TARGETS: { key: CustomFieldTarget; labelKey: string }[] = [
  { key: 'persons', labelKey: 'marketing.customFields.targetPersons' },
  {
    key: 'organizations',
    labelKey: 'marketing.customFields.targetOrganizations'
  }
];

const TYPES: CustomFieldType[] = [
  'string',
  'int',
  'float',
  'bool',
  'date',
  'datetime',
  'enum',
  'multi_enum'
];

const readTarget = (raw: string | null): CustomFieldTarget =>
  raw === 'organizations' ? 'organizations' : 'persons';

const newField = (): FieldDef => ({
  key: '',
  label: '',
  type: 'string',
  required: false,
  options: []
});

const CustomFieldsPage: React.FC = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const target = readTarget(searchParams.get('target'));

  const { data: schema, isLoading } = useGetCustomFieldSchemaQuery(target);
  const [upsert, upsertState] = useUpsertCustomFieldSchemaMutation();
  const [deleteSchema] = useDeleteCustomFieldSchemaMutation();

  const [fields, setFields] = useState<FieldDef[]>([]);
  const [allowUnknown, setAllowUnknown] = useState(false);

  // Sync local form state with fetched schema when target changes.
  useEffect(() => {
    if (schema) {
      setFields(schema.fields ?? []);
      setAllowUnknown(!!schema.allowUnknownFields);
    } else {
      setFields([]);
      setAllowUnknown(false);
    }
  }, [schema, target]);

  const onTargetChange = (key: string | null) => {
    const next = readTarget(key);
    const sp = new URLSearchParams(searchParams);
    if (next === 'persons') sp.delete('target');
    else sp.set('target', next);
    setSearchParams(sp, { replace: true });
  };

  const updateField = (i: number, patch: Partial<FieldDef>) => {
    setFields(prev =>
      prev.map((f, idx) => (idx === i ? { ...f, ...patch } : f))
    );
  };
  const removeField = (i: number) => {
    setFields(prev => prev.filter((_, idx) => idx !== i));
  };

  const updateOption = (fieldIdx: number, optIdx: number, value: string) => {
    setFields(prev =>
      prev.map((f, i) => {
        if (i !== fieldIdx) return f;
        const opts = [...(f.options ?? [])];
        opts[optIdx] = { ...opts[optIdx], value };
        return { ...f, options: opts };
      })
    );
  };
  const addOption = (i: number) => {
    setFields(prev =>
      prev.map((f, idx) =>
        idx === i ? { ...f, options: [...(f.options ?? []), { value: '' }] } : f
      )
    );
  };

  const onSave = async () => {
    await upsert({
      targetCollection: target,
      fields,
      allowUnknownFields: allowUnknown
    });
  };

  const onDelete = async () => {
    if (!schema) return;
    const targetLabel =
      target === 'persons'
        ? t('marketing.customFields.targetPersons')
        : t('marketing.customFields.targetOrganizations');
    if (
      !window.confirm(
        t('marketing.customFields.confirmDelete', { target: targetLabel })
      )
    )
      return;
    await deleteSchema(target);
    setFields([]);
    setAllowUnknown(false);
  };

  return (
    <>
      <div className="mb-3">
        <h3 className="fw-normal mb-1">{t('marketing.customFields.title')}</h3>
        <p className="fs-10 text-muted mb-0">
          {t('marketing.customFields.subtitle')}
        </p>
      </div>

      <Card>
        <Tab.Container activeKey={target} onSelect={onTargetChange}>
          <Card.Header className="border-bottom-0">
            <Nav variant="tabs" className="border-0">
              {TARGETS.map(tg => (
                <Nav.Item key={tg.key}>
                  <Nav.Link eventKey={tg.key}>{t(tg.labelKey)}</Nav.Link>
                </Nav.Item>
              ))}
            </Nav>
          </Card.Header>
          <Card.Body>
            {isLoading ? (
              <div className="text-muted">
                {t('marketing.customFields.loading')}
              </div>
            ) : (
              <>
                {schema && (
                  <Alert variant="light" className="mb-3">
                    <Trans
                      i18nKey="marketing.customFields.schemaBanner"
                      values={{
                        version: schema.version,
                        updatedAt: new Date(schema.updatedAt).toLocaleString()
                      }}
                      components={{ strong: <strong /> }}
                    />
                  </Alert>
                )}

                <Form.Check
                  type="switch"
                  id="allowUnknown"
                  label={t('marketing.customFields.allowUnknownLabel')}
                  checked={allowUnknown}
                  onChange={e => setAllowUnknown(e.target.checked)}
                  className="mb-3"
                />

                <Table responsive size="sm">
                  <thead>
                    <tr>
                      <th>{t('marketing.customFields.colKey')}</th>
                      <th>{t('marketing.customFields.colLabel')}</th>
                      <th>{t('marketing.customFields.colType')}</th>
                      <th>{t('marketing.customFields.colRequired')}</th>
                      <th>{t('marketing.customFields.colOptions')}</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {fields.map((f, i) => (
                      <tr key={i}>
                        <td>
                          <Form.Control
                            size="sm"
                            value={f.key}
                            onChange={e =>
                              updateField(i, { key: e.target.value })
                            }
                            placeholder={t(
                              'marketing.customFields.placeholderKey'
                            )}
                          />
                        </td>
                        <td>
                          <Form.Control
                            size="sm"
                            value={f.label ?? ''}
                            onChange={e =>
                              updateField(i, { label: e.target.value })
                            }
                            placeholder={t(
                              'marketing.customFields.placeholderLabel'
                            )}
                          />
                        </td>
                        <td>
                          <Form.Select
                            size="sm"
                            value={f.type}
                            onChange={e =>
                              updateField(i, {
                                type: e.target.value as CustomFieldType
                              })
                            }
                          >
                            {TYPES.map(tp => (
                              <option key={tp} value={tp}>
                                {tp}
                              </option>
                            ))}
                          </Form.Select>
                        </td>
                        <td className="text-center">
                          <Form.Check
                            checked={!!f.required}
                            onChange={e =>
                              updateField(i, { required: e.target.checked })
                            }
                          />
                        </td>
                        <td>
                          {f.type === 'enum' || f.type === 'multi_enum' ? (
                            <>
                              {(f.options ?? []).map((o, oi) => (
                                <Form.Control
                                  key={oi}
                                  size="sm"
                                  className="mb-1"
                                  value={o.value}
                                  onChange={e =>
                                    updateOption(i, oi, e.target.value)
                                  }
                                  placeholder={t(
                                    'marketing.customFields.placeholderOption'
                                  )}
                                />
                              ))}
                              <Button
                                size="sm"
                                variant="link"
                                className="px-0"
                                onClick={() => addOption(i)}
                              >
                                {t('marketing.customFields.addOption')}
                              </Button>
                            </>
                          ) : (
                            <small className="text-muted">—</small>
                          )}
                        </td>
                        <td>
                          <Button
                            size="sm"
                            variant="link"
                            className="text-danger"
                            onClick={() => removeField(i)}
                          >
                            {t('marketing.customFields.remove')}
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
                <Button
                  size="sm"
                  variant="outline-primary"
                  onClick={() => setFields([...fields, newField()])}
                >
                  {t('marketing.customFields.addField')}
                </Button>
              </>
            )}
          </Card.Body>
          <Card.Footer className="d-flex justify-content-end gap-2">
            {schema && (
              <Button
                variant="outline-danger"
                size="sm"
                onClick={onDelete}
                disabled={upsertState.isLoading}
              >
                {t('marketing.customFields.deleteSchema')}
              </Button>
            )}
            <Button
              variant="primary"
              size="sm"
              onClick={onSave}
              disabled={upsertState.isLoading}
            >
              {schema
                ? t('marketing.customFields.saveChanges')
                : t('marketing.customFields.createSchema')}
            </Button>
          </Card.Footer>
        </Tab.Container>
      </Card>
    </>
  );
};

export default CustomFieldsPage;
