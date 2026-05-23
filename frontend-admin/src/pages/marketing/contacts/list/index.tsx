// Contacts list page — Persons + Organizations behind URL-synced tabs.
// Each tab owns an independent useAdvanceTable instance (see sibling
// PersonsTable / OrganizationsTable) so global filter and pagination
// state survive tab switches.

import { Card, Nav, Tab, Badge } from 'react-bootstrap';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  useListMarketingPersonsQuery,
  useListMarketingOrgsQuery
} from 'store/api/marketingApi';
import PersonsTable from './PersonsTable';
import OrganizationsTable from './OrganizationsTable';

type TabKey = 'persons' | 'organizations';
const DEFAULT_TAB: TabKey = 'persons';

const readTab = (raw: string | null | undefined): TabKey =>
  raw === 'organizations' ? 'organizations' : 'persons';

const ContactsListPage: React.FC = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = readTab(searchParams.get('tab'));

  // Only the counts are read here — the actual tables fire their own
  // queries (RTK Query dedupes them, so this is free).
  const { data: persons } = useListMarketingPersonsQuery(undefined);
  const { data: orgs } = useListMarketingOrgsQuery(undefined);

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
        <h3 className="fw-normal mb-1">{t('marketing.contacts.title')}</h3>
        <p className="fs-10 text-muted mb-0">
          {t('marketing.contacts.list.subtitle')}
        </p>
      </div>
      <Card>
        <Tab.Container activeKey={tab} onSelect={onTabChange}>
          <Card.Header className="border-bottom-0 px-0">
            <Nav variant="tabs" className="border-0 px-x1">
              <Nav.Item>
                <Nav.Link eventKey="persons">
                  {t('marketing.contacts.list.tabPersons')}{' '}
                  {persons?.meta?.count !== undefined && (
                    <Badge bg="secondary" pill>
                      {persons.meta.count}
                    </Badge>
                  )}
                </Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="organizations">
                  {t('marketing.contacts.list.tabOrganizations')}{' '}
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
                {tab === 'persons' && <PersonsTable />}
              </Tab.Pane>
              <Tab.Pane eventKey="organizations">
                {tab === 'organizations' && <OrganizationsTable />}
              </Tab.Pane>
            </Tab.Content>
          </Card.Body>
        </Tab.Container>
      </Card>
    </>
  );
};

export default ContactsListPage;
