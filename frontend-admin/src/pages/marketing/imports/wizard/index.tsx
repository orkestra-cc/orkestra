// Import wizard — pick adapter (csv / excel / odoo), supply the
// payload + mapping, submit to the async runner. The marketingApi
// slice does NOT expose runImport because multipart uploads don't
// fit cleanly through the RTK Query fetchBaseQuery serializer; the
// wizard calls fetch() directly with credentials:'include' and then
// invalidates the relevant tags.
//
// Phase 3 behavior: POST returns 202 Accepted + a jobUuid. The
// wizard's success card surfaces the queued state + a deep link to
// the imports list where the operator polls for completion.

import { useState } from 'react';
import { useNavigate, Link } from 'react-router';
import { Alert, Button, Card, Form, ProgressBar } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import runtimeConfig from 'config/environment';
import { baseApi } from 'store/api/baseApi';
import type { ImportJob, OdooImportConfig } from 'types/marketing';

type Adapter = 'csv' | 'excel' | 'odoo';

const SAMPLE_MAPPING = `{
  "columns": {
    "Email": "person.email",
    "First Name": "person.firstName",
    "Last Name": "person.lastName",
    "Company": "org.legalName",
    "VAT": "org.vat",
    "Tags": "tags"
  },
  "options": {
    "delimiter": ",",
    "hasHeaderRow": "true"
  }
}`;

const SAMPLE_EXCEL_MAPPING = `{
  "columns": {
    "Email": "person.email",
    "Company": "org.legalName",
    "VAT": "org.vat"
  },
  "options": {
    "sheet": "Contacts",
    "headerRow": "1",
    "hasHeaderRow": "true"
  }
}`;

const ODOO_PLACEHOLDER_MAPPING = `{"columns":{}}`;

const CANONICAL_KEYS = [
  'org.legalName',
  'org.vat',
  'org.taxCode',
  'org.kind',
  'org.website',
  'org.email',
  'org.phone',
  'person.firstName',
  'person.lastName',
  'person.email',
  'person.phone',
  'person.title',
  'person.language',
  'role',
  'department',
  'tags',
  'notes',
  'customField.<key>'
];

const acceptForAdapter = (a: Adapter): string => {
  switch (a) {
    case 'csv':
      return '.csv,text/csv';
    case 'excel':
      return '.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
    case 'odoo':
      return '';
  }
};

const ImportWizardPage: React.FC = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const navigate = useNavigate();
  const accessToken = useAppSelector(s => s.auth.accessToken);

  const [adapter, setAdapter] = useState<Adapter>('csv');
  const [file, setFile] = useState<File | null>(null);
  const [mapping, setMapping] = useState(SAMPLE_MAPPING);
  const [sourceName, setSourceName] = useState('');
  const [odoo, setOdoo] = useState<OdooImportConfig>({
    baseUrl: '',
    database: '',
    apiKey: '',
    pageSize: 200,
    includeEngagement: false,
    engagementSinceDays: 90
  });
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ImportJob | null>(null);

  const onAdapterChange = (next: Adapter) => {
    setAdapter(next);
    setFile(null);
    setError(null);
    setResult(null);
    if (next === 'csv') {
      setMapping(SAMPLE_MAPPING);
    } else if (next === 'excel') {
      setMapping(SAMPLE_EXCEL_MAPPING);
    } else {
      setMapping(ODOO_PLACEHOLDER_MAPPING);
    }
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setResult(null);

    try {
      JSON.parse(mapping);
    } catch (err) {
      setError(
        t('marketing.imports.wizard.errorInvalidJson', {
          message: (err as Error).message
        })
      );
      return;
    }

    if ((adapter === 'csv' || adapter === 'excel') && !file) {
      setError(t('marketing.imports.wizard.errorNoFile'));
      return;
    }
    if (
      adapter === 'odoo' &&
      (!odoo.baseUrl || !odoo.database || !odoo.apiKey)
    ) {
      setError(t('marketing.imports.wizard.errorOdooMissingFields'));
      return;
    }

    setRunning(true);
    try {
      const fd = new FormData();
      if (adapter === 'odoo') {
        // For Odoo, the "file" payload is the JSON config blob.
        const blob = new Blob([JSON.stringify(odoo)], {
          type: 'application/json'
        });
        fd.append('file', blob, 'odoo-config.json');
      } else if (file) {
        fd.append('file', file);
      }
      fd.append('mapping', mapping);
      fd.append('importer', adapter);
      if (sourceName) fd.append('sourceName', sourceName);

      const headers: Record<string, string> = {};
      if (accessToken) headers.Authorization = `Bearer ${accessToken}`;
      const res = await fetch(`${runtimeConfig.apiUrl}/v1/marketing/imports`, {
        method: 'POST',
        credentials: 'include',
        headers,
        body: fd
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `${res.status} ${res.statusText}`);
      }
      const body = (await res.json()) as ImportJob;
      setResult(body);
      dispatch(
        baseApi.util.invalidateTags([
          { type: 'MarketingImport', id: 'LIST' },
          { type: 'MarketingOrg', id: 'LIST' },
          { type: 'MarketingPerson', id: 'LIST' }
        ])
      );
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setRunning(false);
    }
  };

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">
            {t('marketing.imports.wizard.heading')}
          </h3>
          <p className="fs-10 text-muted mb-0">
            {t('marketing.imports.wizard.subtitle')}
          </p>
        </div>
        <Link to="/marketing/imports" className="text-muted">
          {t('marketing.imports.wizard.allImports')}
        </Link>
      </div>

      <Card>
        <Card.Body>
          <Form onSubmit={onSubmit}>
            <Form.Group className="mb-3">
              <Form.Label>{t('marketing.imports.adapter.label')}</Form.Label>
              <div className="d-flex gap-3">
                {(['csv', 'excel', 'odoo'] as Adapter[]).map(a => (
                  <Form.Check
                    key={a}
                    type="radio"
                    inline
                    name="adapter"
                    id={`adapter-${a}`}
                    label={t(`marketing.imports.adapter.${a}`)}
                    checked={adapter === a}
                    onChange={() => onAdapterChange(a)}
                    disabled={running}
                  />
                ))}
              </div>
              <Form.Text className="text-muted">
                {t(`marketing.imports.adapter.${adapter}Help`)}
              </Form.Text>
            </Form.Group>

            {(adapter === 'csv' || adapter === 'excel') && (
              <Form.Group className="mb-3">
                <Form.Label>
                  {t('marketing.imports.wizard.fileLabel')}
                </Form.Label>
                <Form.Control
                  type="file"
                  accept={acceptForAdapter(adapter)}
                  onChange={e => {
                    const f = (e.target as HTMLInputElement).files?.[0] ?? null;
                    setFile(f);
                    if (f && !sourceName) setSourceName(f.name);
                  }}
                  disabled={running}
                />
                <Form.Text className="text-muted">
                  {t('marketing.imports.wizard.fileHelp')}
                </Form.Text>
              </Form.Group>
            )}

            {adapter === 'odoo' && (
              <Card className="mb-3 bg-light">
                <Card.Body>
                  <h6 className="mb-3">
                    {t('marketing.imports.odoo.heading')}
                  </h6>
                  <Form.Group className="mb-2">
                    <Form.Label className="fs-10">
                      {t('marketing.imports.odoo.baseUrl')}
                    </Form.Label>
                    <Form.Control
                      size="sm"
                      value={odoo.baseUrl}
                      placeholder="https://my-tenant.odoo.com"
                      onChange={e =>
                        setOdoo({ ...odoo, baseUrl: e.target.value })
                      }
                      disabled={running}
                    />
                  </Form.Group>
                  <Form.Group className="mb-2">
                    <Form.Label className="fs-10">
                      {t('marketing.imports.odoo.database')}
                    </Form.Label>
                    <Form.Control
                      size="sm"
                      value={odoo.database}
                      onChange={e =>
                        setOdoo({ ...odoo, database: e.target.value })
                      }
                      disabled={running}
                    />
                  </Form.Group>
                  <Form.Group className="mb-2">
                    <Form.Label className="fs-10">
                      {t('marketing.imports.odoo.apiKey')}
                    </Form.Label>
                    <Form.Control
                      size="sm"
                      type="password"
                      value={odoo.apiKey}
                      onChange={e =>
                        setOdoo({ ...odoo, apiKey: e.target.value })
                      }
                      disabled={running}
                    />
                  </Form.Group>
                  <div className="d-flex gap-2 align-items-end">
                    <Form.Group style={{ maxWidth: 140 }}>
                      <Form.Label className="fs-10">
                        {t('marketing.imports.odoo.pageSize')}
                      </Form.Label>
                      <Form.Control
                        size="sm"
                        type="number"
                        min={1}
                        max={500}
                        value={odoo.pageSize ?? 200}
                        onChange={e =>
                          setOdoo({
                            ...odoo,
                            pageSize: Number(e.target.value) || 200
                          })
                        }
                        disabled={running}
                      />
                    </Form.Group>
                    <Form.Group className="ms-3">
                      <Form.Check
                        type="checkbox"
                        id="odoo-include-engagement"
                        label={t('marketing.imports.odoo.includeEngagement')}
                        checked={odoo.includeEngagement ?? false}
                        onChange={e =>
                          setOdoo({
                            ...odoo,
                            includeEngagement: e.target.checked
                          })
                        }
                        disabled={running}
                      />
                    </Form.Group>
                  </div>
                  <Form.Text className="text-muted">
                    {t('marketing.imports.odoo.help')}
                  </Form.Text>
                </Card.Body>
              </Card>
            )}

            <Form.Group className="mb-3">
              <Form.Label>
                {t('marketing.imports.wizard.sourceNameLabel')}
              </Form.Label>
              <Form.Control
                value={sourceName}
                onChange={e => setSourceName(e.target.value)}
                placeholder={t(
                  'marketing.imports.wizard.sourceNamePlaceholder'
                )}
                disabled={running}
              />
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>
                {t('marketing.imports.wizard.mappingLabel')}
              </Form.Label>
              <Form.Control
                as="textarea"
                rows={10}
                value={mapping}
                onChange={e => setMapping(e.target.value)}
                disabled={running}
                style={{ fontFamily: 'monospace', fontSize: 12 }}
              />
              <Form.Text className="text-muted">
                {adapter === 'odoo'
                  ? t('marketing.imports.wizard.mappingHelpOdoo')
                  : t('marketing.imports.wizard.mappingHelp', {
                      keys: CANONICAL_KEYS.join(', ')
                    })}
              </Form.Text>
            </Form.Group>

            {error && <Alert variant="danger">{error}</Alert>}
            {running && (
              <div className="mb-3">
                <ProgressBar animated now={100} />
                <small className="text-muted">
                  {t('marketing.imports.wizard.progressLabel')}
                </small>
              </div>
            )}

            <div className="d-flex justify-content-end gap-2">
              <Button type="submit" variant="primary" disabled={running}>
                {t('marketing.imports.wizard.runImport')}
              </Button>
            </div>
          </Form>
        </Card.Body>
      </Card>

      {result && (
        <Card className="mt-3 border-success">
          <Card.Body>
            <h5 className="mb-2">
              {t('marketing.imports.wizard.result.headingQueued')}
            </h5>
            <p className="text-muted">
              {t('marketing.imports.wizard.result.queuedSubtitle')}
            </p>
            <div className="mb-2 text-muted fs-10">
              {t('marketing.imports.wizard.result.jobLabel')}{' '}
              <code>{result.uuid}</code> ·{' '}
              {t('marketing.imports.wizard.result.statusLabel')}{' '}
              <code>{result.status}</code>
            </div>
            <div className="d-flex gap-2">
              <Button
                variant="outline-primary"
                size="sm"
                onClick={() => navigate('/marketing/imports')}
              >
                {t('marketing.imports.wizard.result.backToList')}
              </Button>
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={() => {
                  setFile(null);
                  setResult(null);
                  setError(null);
                }}
              >
                {t('marketing.imports.wizard.result.runAnother')}
              </Button>
            </div>
          </Card.Body>
        </Card>
      )}
    </>
  );
};

export default ImportWizardPage;
