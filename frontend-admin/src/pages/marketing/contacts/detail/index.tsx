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
import { Trans, useTranslation } from 'react-i18next';
import {
  useGetMarketingPersonQuery,
  useListPersonMembershipsQuery,
  useListMarketingOrgsQuery,
  useListMarketingTagsQuery
} from 'store/api/marketingApi';
import TimelineTab from './TimelineTab';
import ScoresTab from './ScoresTab';
import CardsTab from './CardsTab';

type TabKey =
  | 'overview'
  | 'memberships'
  | 'timeline'
  | 'scores'
  | 'cards'
  | 'sources';
const DEFAULT_TAB: TabKey = 'overview';

const readTab = (raw: string | null | undefined): TabKey => {
  if (raw === 'memberships') return 'memberships';
  if (raw === 'timeline') return 'timeline';
  if (raw === 'scores') return 'scores';
  if (raw === 'cards') return 'cards';
  if (raw === 'sources') return 'sources';
  return 'overview';
};

const ContactDetailPage: React.FC = () => {
  const { t } = useTranslation();
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
    return (
      <div className="p-3 text-muted">
        {t('marketing.contacts.detail.loading')}
      </div>
    );
  }
  if (error || !person) {
    return (
      <Card>
        <Card.Body>
          <h5 className="mb-2">
            {t('marketing.contacts.detail.notFoundTitle')}
          </h5>
          <p className="text-muted mb-0">
            <Trans
              i18nKey="marketing.contacts.detail.notFoundBody"
              values={{ id }}
              components={{ code: <code /> }}
            />
          </p>
          <Link to="/marketing/contacts" className="btn btn-link px-0 mt-2">
            {t('marketing.contacts.detail.backToContacts')}
          </Link>
        </Card.Body>
      </Card>
    );
  }

  const fullName =
    [person.firstName, person.lastName].filter(Boolean).join(' ') ||
    t('marketing.contacts.detail.unnamed');
  const dash = t('marketing.contacts.detail.dash');

  return (
    <>
      <div className="mb-3 d-flex align-items-baseline gap-3">
        <h3 className="fw-normal mb-0">{fullName}</h3>
        {person.title && <span className="text-muted">{person.title}</span>}
        <Link to="/marketing/contacts" className="ms-auto text-muted">
          {t('marketing.contacts.detail.allContacts')}
        </Link>
      </div>

      <Card>
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom-0">
            <Nav variant="tabs" className="border-0">
              <Nav.Item>
                <Nav.Link eventKey="overview">
                  {t('marketing.contacts.detail.tabs.overview')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="memberships">
                  {t('marketing.contacts.detail.tabs.memberships')}{' '}
                  {memberships?.items?.length ? (
                    <Badge bg="secondary" pill>
                      {memberships.items.length}
                    </Badge>
                  ) : null}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="timeline">
                  {t('marketing.contacts.detail.tabs.timeline')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="scores">
                  {t('marketing.contacts.detail.tabs.scores')}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="cards">
                  {t('marketing.contacts.detail.tabs.cards')}{' '}
                  {person.activeCardUuids?.length ? (
                    <Badge bg="secondary" pill>
                      {person.activeCardUuids.length}
                    </Badge>
                  ) : null}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="sources">
                  {t('marketing.contacts.detail.tabs.sources')}{' '}
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
                    <h6 className="text-muted">
                      {t('marketing.contacts.detail.emailsHeader')}
                    </h6>
                    {person.emails?.length ? (
                      <ul className="list-unstyled mb-3">
                        {person.emails.map((e, i) => (
                          <li key={i} className="mb-1">
                            {e.address}
                            {e.primary && (
                              <Badge bg="primary" pill className="ms-2">
                                {t('marketing.contacts.detail.badgePrimary')}
                              </Badge>
                            )}
                            {e.verified && (
                              <Badge bg="success" pill className="ms-1">
                                {t('marketing.contacts.detail.badgeVerified')}
                              </Badge>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-muted mb-3">
                        {t('marketing.contacts.detail.emailsEmpty')}
                      </p>
                    )}
                  </Col>
                  <Col md={6}>
                    <h6 className="text-muted">
                      {t('marketing.contacts.detail.phonesHeader')}
                    </h6>
                    {person.phones?.length ? (
                      <ul className="list-unstyled mb-3">
                        {person.phones.map((p, i) => (
                          <li key={i}>
                            {p.number}{' '}
                            {p.primary && (
                              <Badge bg="primary" pill className="ms-2">
                                {t('marketing.contacts.detail.badgePrimary')}
                              </Badge>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="text-muted mb-3">
                        {t('marketing.contacts.detail.phonesEmpty')}
                      </p>
                    )}
                  </Col>
                </Row>
                <Row>
                  <Col md={6}>
                    <h6 className="text-muted">
                      {t('marketing.contacts.detail.tagsHeader')}
                    </h6>
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
                      <p className="text-muted mb-3">
                        {t('marketing.contacts.detail.tagsEmpty')}
                      </p>
                    )}
                  </Col>
                  <Col md={6}>
                    <h6 className="text-muted">
                      {t('marketing.contacts.detail.languageHeader')}
                    </h6>
                    <p className="mb-3">{person.language || dash}</p>
                  </Col>
                </Row>
                {person.customFields &&
                  Object.keys(person.customFields).length > 0 && (
                    <>
                      <h6 className="text-muted">
                        {t('marketing.contacts.detail.customFieldsHeader')}
                      </h6>
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
                    {t('marketing.contacts.detail.memberships.empty')}
                  </p>
                ) : (
                  <Table size="sm" responsive>
                    <thead>
                      <tr>
                        <th>
                          {t('marketing.contacts.detail.memberships.colOrg')}
                        </th>
                        <th>
                          {t('marketing.contacts.detail.memberships.colRole')}
                        </th>
                        <th>
                          {t('marketing.contacts.detail.memberships.colStatus')}
                        </th>
                        <th>
                          {t('marketing.contacts.detail.memberships.colPeriod')}
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {memberships.items.map(m => (
                        <tr key={m.uuid}>
                          <td>
                            {orgsByUUID[m.orgUuid]?.legalName ??
                              m.orgUuid.slice(0, 8)}
                          </td>
                          <td>{m.role || dash}</td>
                          <td>
                            {m.active ? (
                              <Badge bg="success">
                                {t(
                                  'marketing.contacts.detail.memberships.statusActive'
                                )}
                              </Badge>
                            ) : (
                              <Badge bg="secondary">
                                {t(
                                  'marketing.contacts.detail.memberships.statusClosed'
                                )}
                              </Badge>
                            )}{' '}
                            {m.primary && (
                              <Badge bg="primary">
                                {t(
                                  'marketing.contacts.detail.memberships.primary'
                                )}
                              </Badge>
                            )}
                          </td>
                          <td>
                            <small className="text-muted">
                              {m.since
                                ? new Date(m.since).toLocaleDateString()
                                : dash}{' '}
                              →{' '}
                              {m.until
                                ? new Date(m.until).toLocaleDateString()
                                : t(
                                    'marketing.contacts.detail.memberships.present'
                                  )}
                            </small>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                )}
              </Tab.Pane>

              <Tab.Pane eventKey="timeline" mountOnEnter unmountOnExit>
                <TimelineTab personId={id} />
              </Tab.Pane>

              <Tab.Pane eventKey="scores" mountOnEnter unmountOnExit>
                <ScoresTab personId={id} />
              </Tab.Pane>

              <Tab.Pane eventKey="cards" mountOnEnter unmountOnExit>
                <CardsTab personId={id} />
              </Tab.Pane>

              <Tab.Pane eventKey="sources">
                <p className="text-muted">
                  {t('marketing.contacts.detail.sources.description')}
                </p>
                {!person.sources?.length ? (
                  <p className="text-muted mb-0">
                    {t('marketing.contacts.detail.sources.empty')}
                  </p>
                ) : (
                  <Table size="sm" responsive>
                    <thead>
                      <tr>
                        <th>
                          {t('marketing.contacts.detail.sources.colImporter')}
                        </th>
                        <th>{t('marketing.contacts.detail.sources.colJob')}</th>
                        <th>
                          {t('marketing.contacts.detail.sources.colExternalId')}
                        </th>
                        <th>
                          {t('marketing.contacts.detail.sources.colImportedAt')}
                        </th>
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
                              dash
                            )}
                          </td>
                          <td>
                            <small>{s.externalId || dash}</small>
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
          {t('marketing.contacts.detail.editDisabled')}
        </Button>
      </div>
    </>
  );
};

export default ContactDetailPage;
