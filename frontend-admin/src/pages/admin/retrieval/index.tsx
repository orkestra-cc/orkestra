import { Alert, Card, Nav, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faClockRotateLeft,
  faDatabase,
  faLayerGroup
} from '@fortawesome/free-solid-svg-icons';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import CorporaTab from './CorporaTab';
import IndexingTab from './IndexingTab';
import SearchLogTab from './SearchLogTab';
import { useRetrievalPermissions } from './retrievalPermissions';

// /admin/retrieval — the operator surface of the core retrieval module
// (ADR-0023): the installation-wide corpus report with diagnostics and the
// orphan drop, the active organization's indexing progress, and its audited
// search log. Tabs follow the url-tabs convention (?tab=…). Each tab needs
// its own permission and the page re-checks them rather than trusting the
// menu entry: an org owner or admin without a platform role reaches the
// search log only by URL (the nav item is administrator-only). An unknown
// value, or a tab the caller may not open, degrades to the first visible
// tab; with none visible the page says so. Each tab mounts only while
// active, so its queries — the search log's is an audited read — fire only
// when it shows.
const TABS = [
  {
    key: 'corpora',
    labelKey: 'adminRetrieval.tabs.corpora',
    icon: faDatabase
  },
  {
    key: 'indexing',
    labelKey: 'adminRetrieval.tabs.indexing',
    icon: faLayerGroup
  },
  {
    key: 'search-log',
    labelKey: 'adminRetrieval.tabs.searchLog',
    icon: faClockRotateLeft
  }
] as const;
type RetrievalTab = (typeof TABS)[number];

const readTab = (param: string | null, visible: readonly RetrievalTab[]) =>
  (visible.find(tab => tab.key === param) ?? visible[0])?.key;

const RetrievalAdminPage = () => {
  const { t } = useTranslation();
  const { canReadCorpora, canReadTenant, canReadSearchLog } =
    useRetrievalPermissions();
  const [searchParams, setSearchParams] = useSearchParams();
  const allowed: Record<RetrievalTab['key'], boolean> = {
    corpora: canReadCorpora,
    indexing: canReadTenant,
    'search-log': canReadSearchLog
  };
  const visibleTabs = TABS.filter(tab => allowed[tab.key]);
  const activeTab = readTab(searchParams.get('tab'), visibleTabs);

  const handleTabSelect = (key: string | null) => {
    const next = readTab(key, visibleTabs);
    if (!key || !next) return;
    setSearchParams(
      prev => {
        prev.set('tab', next);
        return prev;
      },
      { replace: true }
    );
  };

  return (
    <>
      <PageHeader
        title={t('adminRetrieval.pageTitle')}
        description={t('adminRetrieval.pageSubtitle')}
        className="mb-3"
      />
      {!activeTab ? (
        <Alert variant="info" className="fs-10">
          {t('adminRetrieval.noAccess')}
        </Alert>
      ) : (
        <Card className="shadow-none border">
          <Tab.Container activeKey={activeTab} onSelect={handleTabSelect}>
            {/* Horizontal overflow instead of wrapping on narrow viewports;
                every tab keeps its visible text label. */}
            <Card.Header className="border-bottom border-200 overflow-auto">
              <Nav
                variant="tabs"
                className="card-header-tabs fs-10 flex-nowrap"
              >
                {visibleTabs.map(tab => (
                  <Nav.Item key={tab.key}>
                    <Nav.Link eventKey={tab.key} className="text-nowrap">
                      <FontAwesomeIcon icon={tab.icon} className="me-2" />
                      {t(tab.labelKey)}
                    </Nav.Link>
                  </Nav.Item>
                ))}
              </Nav>
            </Card.Header>
            <Card.Body>
              <Tab.Content>
                {canReadCorpora && (
                  <Tab.Pane eventKey="corpora" mountOnEnter unmountOnExit>
                    <CorporaTab />
                  </Tab.Pane>
                )}
                {canReadTenant && (
                  <Tab.Pane eventKey="indexing" mountOnEnter unmountOnExit>
                    <IndexingTab />
                  </Tab.Pane>
                )}
                {canReadSearchLog && (
                  <Tab.Pane eventKey="search-log" mountOnEnter unmountOnExit>
                    <SearchLogTab />
                  </Tab.Pane>
                )}
              </Tab.Content>
            </Card.Body>
          </Tab.Container>
        </Card>
      )}
    </>
  );
};

export default RetrievalAdminPage;
