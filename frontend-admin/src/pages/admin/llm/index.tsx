import { Alert, Card, Nav, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faKey, faRobot } from '@fortawesome/free-solid-svg-icons';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import CredentialsTab from './CredentialsTab';
import ModelsTab from './ModelsTab';
import { useLlmPermissions } from './llmPermissions';

// /admin/llm — the operator surface of the core llm module (ADR-0022):
// the org's provider credentials, its configured models and who may use
// them. Tabs follow the url-tabs convention (?tab=…); PR 2 adds usage and
// budget, PR 3 accounts. An unknown value degrades to the models tab. Each
// tab re-checks llm.admin.read itself rather than trusting the menu entry,
// and mounts only while active so a tab's queries fire only when it shows.
const TABS = [
  { key: 'models', labelKey: 'adminLlm.tabs.models', icon: faRobot },
  { key: 'credentials', labelKey: 'adminLlm.tabs.credentials', icon: faKey }
] as const;
type TabKey = (typeof TABS)[number]['key'];
const DEFAULT_TAB: TabKey = 'models';

const readTab = (param: string | null): TabKey =>
  TABS.find(tab => tab.key === param)?.key ?? DEFAULT_TAB;

const LlmAdminPage = () => {
  const { t } = useTranslation();
  const { canRead } = useLlmPermissions();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = readTab(searchParams.get('tab'));

  const handleTabSelect = (key: string | null) => {
    if (!key) return;
    setSearchParams(
      prev => {
        prev.set('tab', readTab(key));
        return prev;
      },
      { replace: true }
    );
  };

  return (
    <>
      <PageHeader
        title={t('adminLlm.pageTitle')}
        description={t('adminLlm.pageSubtitle')}
        className="mb-3"
      />
      {!canRead ? (
        <Alert variant="info" className="fs-10">
          {t('adminLlm.noAccess')}
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
                {TABS.map(tab => (
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
                <Tab.Pane eventKey="models" mountOnEnter unmountOnExit>
                  <ModelsTab />
                </Tab.Pane>
                <Tab.Pane eventKey="credentials" mountOnEnter unmountOnExit>
                  <CredentialsTab />
                </Tab.Pane>
              </Tab.Content>
            </Card.Body>
          </Tab.Container>
        </Card>
      )}
    </>
  );
};

export default LlmAdminPage;
