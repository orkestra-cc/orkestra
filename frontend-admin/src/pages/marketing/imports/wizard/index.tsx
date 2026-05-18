// Import wizard — upload CSV + supply column mapping JSON, then run.
// The marketingApi slice does NOT expose runImport because multipart
// uploads don't fit cleanly through the RTK Query fetchBaseQuery
// serializer; the wizard calls fetch() directly with
// credentials:'include' and then invalidates the relevant tags.
//
// Phase 1 UX is intentionally spartan — paste the mapping JSON as
// text. A click-to-map column-picker arrives in a follow-up.

import { useState } from 'react';
import { useNavigate, Link } from 'react-router';
import { Alert, Button, Card, Form, ProgressBar } from 'react-bootstrap';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import runtimeConfig from 'config/environment';
import { baseApi } from 'store/api/baseApi';
import type { ImportJob } from 'types/marketing';

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

const ImportWizardPage: React.FC = () => {
  const dispatch = useAppDispatch();
  const navigate = useNavigate();
  const accessToken = useAppSelector(s => s.auth.accessToken);

  const [file, setFile] = useState<File | null>(null);
  const [mapping, setMapping] = useState(SAMPLE_MAPPING);
  const [sourceName, setSourceName] = useState('');
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ImportJob | null>(null);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setResult(null);
    if (!file) {
      setError('Pick a CSV file first.');
      return;
    }
    try {
      JSON.parse(mapping);
    } catch (err) {
      setError(`Mapping is not valid JSON: ${(err as Error).message}`);
      return;
    }
    setRunning(true);
    try {
      const fd = new FormData();
      fd.append('file', file);
      fd.append('mapping', mapping);
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
      // Invalidate the read caches that just changed.
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
          <h3 className="fw-normal mb-1">New import</h3>
          <p className="fs-10 text-muted mb-0">
            Upload a CSV and supply a column mapping. The pipeline runs
            synchronously — keep the tab open until the result panel appears.
          </p>
        </div>
        <Link to="/marketing/imports" className="text-muted">
          ← All imports
        </Link>
      </div>

      <Card>
        <Card.Body>
          <Form onSubmit={onSubmit}>
            <Form.Group className="mb-3">
              <Form.Label>CSV file</Form.Label>
              <Form.Control
                type="file"
                accept=".csv,text/csv"
                onChange={e => {
                  const f = (e.target as HTMLInputElement).files?.[0] ?? null;
                  setFile(f);
                  if (f && !sourceName) setSourceName(f.name);
                }}
                disabled={running}
              />
              <Form.Text className="text-muted">
                In-memory parse: files up to 32 MB stay in RAM; larger files
                spill to a temp directory.
              </Form.Text>
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>Source name</Form.Label>
              <Form.Control
                value={sourceName}
                onChange={e => setSourceName(e.target.value)}
                placeholder="Defaults to the uploaded filename"
                disabled={running}
              />
            </Form.Group>

            <Form.Group className="mb-3">
              <Form.Label>Column mapping (JSON)</Form.Label>
              <Form.Control
                as="textarea"
                rows={10}
                value={mapping}
                onChange={e => setMapping(e.target.value)}
                disabled={running}
                style={{ fontFamily: 'monospace', fontSize: 12 }}
              />
              <Form.Text className="text-muted">
                Recognised canonical keys: {CANONICAL_KEYS.join(', ')}
              </Form.Text>
            </Form.Group>

            {error && <Alert variant="danger">{error}</Alert>}
            {running && (
              <div className="mb-3">
                <ProgressBar animated now={100} />
                <small className="text-muted">
                  Pipeline is running… do not close the tab.
                </small>
              </div>
            )}

            <div className="d-flex justify-content-end gap-2">
              <Button
                type="submit"
                variant="primary"
                disabled={running || !file}
              >
                Run import
              </Button>
            </div>
          </Form>
        </Card.Body>
      </Card>

      {result && (
        <Card className="mt-3 border-success">
          <Card.Body>
            <h5 className="mb-2">
              Import {result.status === 'done' ? 'completed' : 'finished'}
            </h5>
            <div className="mb-2 text-muted fs-10">
              Job <code>{result.uuid}</code>
            </div>
            <ul className="list-unstyled mb-3">
              <li>Rows read: {result.stats.rowsRead}</li>
              <li>Rows failed: {result.stats.rowsFailed ?? 0}</li>
              <li>
                Organizations: {result.stats.orgsCreated ?? 0} created,{' '}
                {result.stats.orgsMerged ?? 0} merged
              </li>
              <li>
                Persons: {result.stats.personsCreated ?? 0} created,{' '}
                {result.stats.personsMerged ?? 0} merged
              </li>
              <li>Memberships linked: {result.stats.membershipsLinked ?? 0}</li>
              <li>
                Conflicts skipped: {result.stats.conflictsSkipped ?? 0}
                {result.stats.conflictsSkipped ? (
                  <small className="text-warning ms-1">
                    (dedup-key disagreements — Phase 3 review queue will route
                    these for resolution)
                  </small>
                ) : null}
              </li>
            </ul>
            <div className="d-flex gap-2">
              <Button
                variant="outline-primary"
                size="sm"
                onClick={() => navigate('/marketing/imports')}
              >
                Back to imports list
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
                Run another
              </Button>
            </div>
          </Card.Body>
        </Card>
      )}
    </>
  );
};

export default ImportWizardPage;
