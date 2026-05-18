// Contacts list page — Persons + Organizations behind URL-synced tabs.
// Phase 1 is intentionally a thin, table-driven surface so operators
// can verify the contact base before the richer features land.

import { useMemo } from 'react';
import { Card, Nav, Tab, Table, Badge } from 'react-bootstrap';
import { Link, useSearchParams } from 'react-router';
import {
  useListMarketingPersonsQuery,
  useListMarketingOrgsQuery,
  useListMarketingTagsQuery
} from 'store/api/marketingApi';
import type { Person, Organization, Tag } from 'types/marketing';

type TabKey = 'persons' | 'organizations';
const DEFAULT_TAB: TabKey = 'persons';

const readTab = (raw: string | null | undefined): TabKey =>
  raw === 'organizations' ? 'organizations' : 'persons';

const primaryEmail = (p: Person | Organization) =>
  p.emails?.find(e => e.primary)?.address ?? p.emails?.[0]?.address ?? '—';

const fullName = (p: Person) =>
  [p.firstName, p.lastName].filter(Boolean).join(' ') || '—';

const ContactsListPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  const { data: persons, isLoading: personsLoading } =
    useListMarketingPersonsQuery(undefined);
  const { data: orgs, isLoading: orgsLoading } =
    useListMarketingOrgsQuery(undefined);
  const { data: tagsResp } = useListMarketingTagsQuery();

  const tagsByUUID = useMemo(() => {
    const map: Record<string, Tag> = {};
    tagsResp?.items?.forEach(t => {
      map[t.uuid] = t;
    });
    return map;
  }, [tagsResp]);

  const onTabChange = (key: string | null) => {
    const next = readTab(key);
    const sp = new URLSearchParams(searchParams);
    if (next === DEFAULT_TAB) sp.delete('tab');
    else sp.set('tab', next);
    setSearchParams(sp, { replace: true });
  };

  return (
    <>
      <div className="mb-3">
        <h3 className="fw-normal mb-1">Contacts</h3>
        <p className="fs-10 text-muted mb-0">
          Persons and Organizations imported into or created within this
          tenant's marketing base.
        </p>
      </div>
      <Card>
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom-0">
            <Nav variant="tabs" className="border-0">
              <Nav.Item>
                <Nav.Link eventKey="persons">
                  Persons{' '}
                  {persons?.meta?.count !== undefined && (
                    <Badge bg="secondary" pill>
                      {persons.meta.count}
                    </Badge>
                  )}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="organizations">
                  Organizations{' '}
                  {orgs?.meta?.count !== undefined && (
                    <Badge bg="secondary" pill>
                      {orgs.meta.count}
                    </Badge>
                  )}
                </Nav.Link>
              </Nav.Item>
            </Nav>
          </Card.Header>
          <Card.Body className="p-0">
            <Tab.Content>
              <Tab.Pane eventKey="persons">
                {personsLoading ? (
                  <div className="p-3 text-muted">Loading…</div>
                ) : !persons?.items?.length ? (
                  <div className="p-3 text-muted">
                    No persons yet. Run a CSV import to populate the contact
                    base.
                  </div>
                ) : (
                  <Table responsive hover className="mb-0">
                    <thead className="bg-200">
                      <tr>
                        <th>Name</th>
                        <th>Email</th>
                        <th>Tags</th>
                        <th>Updated</th>
                      </tr>
                    </thead>
                    <tbody>
                      {persons.items.map(p => (
                        <tr key={p.uuid}>
                          <td>
                            <Link
                              to={`/marketing/contacts/${p.uuid}`}
                              className="fw-medium"
                            >
                              {fullName(p)}
                            </Link>
                          </td>
                          <td>{primaryEmail(p)}</td>
                          <td>
                            {p.tags?.map(uuid => (
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
                          </td>
                          <td>
                            <small className="text-muted">
                              {new Date(p.updatedAt).toLocaleDateString()}
                            </small>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                )}
              </Tab.Pane>
              <Tab.Pane eventKey="organizations">
                {orgsLoading ? (
                  <div className="p-3 text-muted">Loading…</div>
                ) : !orgs?.items?.length ? (
                  <div className="p-3 text-muted">No organizations yet.</div>
                ) : (
                  <Table responsive hover className="mb-0">
                    <thead className="bg-200">
                      <tr>
                        <th>Legal name</th>
                        <th>Kind</th>
                        <th>VAT</th>
                        <th>Email</th>
                        <th>Updated</th>
                      </tr>
                    </thead>
                    <tbody>
                      {orgs.items.map(o => (
                        <tr key={o.uuid}>
                          <td>
                            <span className="fw-medium">{o.legalName}</span>
                            {o.displayName && o.displayName !== o.legalName && (
                              <div className="text-muted fs-10">
                                {o.displayName}
                              </div>
                            )}
                          </td>
                          <td>
                            <Badge bg="light" text="dark">
                              {o.kind}
                            </Badge>
                          </td>
                          <td>{o.vat || '—'}</td>
                          <td>{primaryEmail(o)}</td>
                          <td>
                            <small className="text-muted">
                              {new Date(o.updatedAt).toLocaleDateString()}
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
    </>
  );
};

export default ContactsListPage;
