// Person detail page — URL-synced tabs (overview, memberships, sources)
// per the url-tabs project skill. Read-mostly in Phase 1; structured
// editing arrives in PR-5 follow-ups.

import {
  Card,
  Nav,
  Tab,
  Table,
  Badge,
  Button,
  Row,
  Col
} from 'react-bootstrap';
import { useParams, useSearchParams, Link } from 'react-router';
import {
  useGetMarketingPersonQuery,
  useListPersonMembershipsQuery,
  useListMarketingOrgsQuery,
  useListMarketingTagsQuery
} from 'store/api/marketingApi';

type TabKey = 'overview' | 'memberships' | 'sources';
const DEFAULT_TAB: TabKey = 'overview';

const readTab = (raw: string | null | undefined): TabKey => {
  if (raw === 'memberships') return 'memberships';
  if (raw === 'sources') return 'sources';
  return 'overview';
};

const ContactDetailPage: React.FC = () => {
  const { id = '' } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  const { data: person, isLoading, error } = useGetMarketingPersonQuery(id);
  const { data: memberships } = useListPersonMembershipsQuery(id, {
    skip: !id
  });
  const { data: orgs } = useListMarketingOrgsQuery(undefined);
  const { data: tagsResp } = useListMarketingTagsQuery();

  const orgsByUUID = Object.fromEntries(
    (orgs?.items ?? []).map(o => [o.uuid, o])
  );
  const tagsByUUID = Object.fromEntries(
    (tagsResp?.items ?? []).map(t => [t.uuid, t])
  );

  const onTabChange = (key: string | null) => {
    const next = readTab(key);
    const sp = new URLSearchParams(searchParams);
    if (next === DEFAULT_TAB) sp.delete('tab');
    else sp.set('tab', next);
    setSearchParams(sp, { replace: true });
  };

  if (isLoading) {
    return <div className="p-3 text-muted">Loading…</div>;
  }
  if (error || !person) {
    return (
      <Card>
        <Card.Body>
          <h5 className="mb-2">Contact not found</h5>
          <p className="text-muted mb-0">
            The person UUID <code>{id}</code> does not exist in this tenant.
          </p>
          <Link to="/marketing/contacts" className="btn btn-link px-0 mt-2">
            ← Back to contacts
          </Link>
        </Card.Body>
      </Card>
    );
  }

  const fullName =
    [person.firstName, person.lastName].filter(Boolean).join(' ') ||
    'Unnamed contact';

  return (
    <>
      <div className="mb-3 d-flex align-items-baseline gap-3">
        <h3 className="fw-normal mb-0">{fullName}</h3>
        {person.title && <span className="text-muted">{person.title}</span>}
        <Link to="/marketing/contacts" className="ms-auto text-muted">
          ← All contacts
        </Link>
      </div>

      <Card>
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom-0">
            <Nav variant="tabs" className="border-0">
              <Nav.Item>
                <Nav.Link eventKey="overview">Overview</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="memberships">
                  Memberships{' '}
                  {memberships?.items?.length ? (
                    <Badge bg="secondary" pill>
                      {memberships.items.length}
                    </Badge>
                  ) : null}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="sources">
                  Sources{' '}
                  {person.sources?.length ? (
                    <Badge bg="secondary" pill>
                      {person.sources.length}
                    </Badge>
                  ) : null}
                </Nav.Link>
              </Nav.Item>
            </Nav>
          </Card.Header>
          <Card.Body>
            <Tab.Content>
              <Tab.Pane eventKey="overview">
                <Row>
                  <Col md={6}>
                    <h6 className="text-muted">Emails</h6>
                    {person.emails?.length ? (
                      <ul className="list-unstyled mb-3">
                        {person.emails.map((e, i) => (
                          <li key={i} className="mb-1">
                            {e.address}
                            {e.primary && (
                              <Badge bg="primary" pill className="ms-2">
                                primary
                              </Badge>
                            )}
                            {e.verified && (
                              <Badge bg="success" pill className="ms-1">
                                verified
                              </Badge>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-muted mb-3">No emails on file.</p>
                    )}
                  </Col>
                  <Col md={6}>
                    <h6 className="text-muted">Phones</h6>
                    {person.phones?.length ? (
                      <ul className="list-unstyled mb-3">
                        {person.phones.map((p, i) => (
                          <li key={i}>
                            {p.number}{' '}
                            {p.primary && (
                              <Badge bg="primary" pill className="ms-2">
                                primary
                              </Badge>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-muted mb-3">No phones on file.</p>
                    )}
                  </Col>
                </Row>
                <Row>
                  <Col md={6}>
                    <h6 className="text-muted">Tags</h6>
                    {person.tags?.length ? (
                      <div className="mb-3">
                        {person.tags.map(uuid => (
                          <Badge
                            key={uuid}
                            bg="info"
                            pill
                            className="me-1"
                            style={{
                              backgroundColor:
                                tagsByUUID[uuid]?.color || undefined
                            }}
                          >
                            {tagsByUUID[uuid]?.name ?? uuid.slice(0, 8)}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <p className="text-muted mb-3">No tags.</p>
                    )}
                  </Col>
                  <Col md={6}>
                    <h6 className="text-muted">Language</h6>
                    <p className="mb-3">{person.language || '—'}</p>
                  </Col>
                </Row>
                {person.customFields &&
                  Object.keys(person.customFields).length > 0 && (
                    <>
                      <h6 className="text-muted">Custom fields</h6>
                      <Table size="sm" className="mb-0">
                        <tbody>
                          {Object.entries(person.customFields).map(([k, v]) => (
                            <tr key={k}>
                              <td className="fw-medium" style={{ width: 200 }}>
                                {k}
                              </td>
                              <td>{String(v)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </Table>
                    </>
                  )}
              </Tab.Pane>

              <Tab.Pane eventKey="memberships">
                {!memberships?.items?.length ? (
                  <p className="text-muted mb-0">
                    No memberships. Link this person to an organization to
                    populate the activity-org denormalization (used in Phase 2+
                    scoring).
                  </p>
                ) : (
                  <Table size="sm" responsive>
                    <thead>
                      <tr>
                        <th>Organization</th>
                        <th>Role</th>
                        <th>Status</th>
                        <th>Period</th>
                      </tr>
                    </thead>
                    <tbody>
                      {memberships.items.map(m => (
                        <tr key={m.uuid}>
                          <td>
                            {orgsByUUID[m.orgUuid]?.legalName ??
                              m.orgUuid.slice(0, 8)}
                          </td>
                          <td>{m.role || '—'}</td>
                          <td>
                            {m.active ? (
                              <Badge bg="success">active</Badge>
                            ) : (
                              <Badge bg="secondary">closed</Badge>
                            )}{' '}
                            {m.primary && <Badge bg="primary">primary</Badge>}
                          </td>
                          <td>
                            <small className="text-muted">
                              {m.since
                                ? new Date(m.since).toLocaleDateString()
                                : '—'}{' '}
                              →{' '}
                              {m.until
                                ? new Date(m.until).toLocaleDateString()
                                : 'present'}
                            </small>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                )}
              </Tab.Pane>

              <Tab.Pane eventKey="sources">
                <p className="text-muted">
                  Every importer run and manual create appends a provenance
                  entry; the array is monotonic so this is the audit log of
                  where this contact's data came from.
                </p>
                {!person.sources?.length ? (
                  <p className="text-muted mb-0">No provenance entries.</p>
                ) : (
                  <Table size="sm" responsive>
                    <thead>
                      <tr>
                        <th>Importer</th>
                        <th>Job</th>
                        <th>External ID</th>
                        <th>Imported at</th>
                      </tr>
                    </thead>
                    <tbody>
                      {person.sources.map((s, i) => (
                        <tr key={i}>
                          <td>
                            <Badge bg="light" text="dark">
                              {s.importer}
                            </Badge>
                          </td>
                          <td>
                            {s.jobUuid ? (
                              <Link to={`/marketing/imports/${s.jobUuid}`}>
                                <small>{s.jobUuid.slice(0, 8)}</small>
                              </Link>
                            ) : (
                              '—'
                            )}
                          </td>
                          <td>
                            <small>{s.externalId || '—'}</small>
                          </td>
                          <td>
                            <small className="text-muted">
                              {new Date(s.importedAt).toLocaleString()}
                            </small>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                )}
              </Tab.Pane>
            </Tab.Content>
          </Card.Body>
        </Tab.Container>
      </Card>

      <div className="mt-3 d-flex justify-content-end">
        <Button variant="outline-secondary" size="sm" disabled>
          Edit (coming in Phase 1 follow-up)
        </Button>
      </div>
    </>
  );
};

export default ContactDetailPage;
