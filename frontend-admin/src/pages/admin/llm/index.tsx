import { Alert, Card, Nav, Tab } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faChartColumn,
  faCoins,
  faKey,
  faRobot,
  faUserShield
} from '@fortawesome/free-solid-svg-icons';
import { useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import AccountsTab from './AccountsTab';
import BudgetTab from './BudgetTab';
import CredentialsTab from './CredentialsTab';
import ModelsTab from './ModelsTab';
import UsageTab from './UsageTab';
import { useLlmPermissions } from './llmPermissions';

// /admin/llm — the operator surface of the core llm module (ADR-0022):
// the org's provider credentials, its configured models and who may use
// them, the month's usage and the budget. Tabs follow the url-tabs
// convention (?tab=…); the accounts tab lists the members' linked ChatGPT
// accounts (Sign in with ChatGPT). An unknown value — or a tab the
// caller may not open — degrades to the models tab. The page re-checks
// llm.admin.read itself rather than trusting the menu entry and hides the
// tabs without it; the usage tab additionally needs llm.usage.read (the
// budget is read with llm.admin.read, saved with llm.budgets.admin). Each
// tab mounts only while active, so its queries fire only when it shows.
const TABS = [
  { key: 'models', labelKey: 'adminLlm.tabs.models', icon: faRobot },
  { key: 'credentials', labelKey: 'adminLlm.tabs.credentials', icon: faKey },
  { key: 'accounts', labelKey: 'adminLlm.tabs.accounts', icon: faUserShield },
  { key: 'usage', labelKey: 'adminLlm.tabs.usage', icon: faChartColumn },
  { key: 'budget', labelKey: 'adminLlm.tabs.budget', icon: faCoins }
] as const;
type TabKey = (typeof TABS)[number]['key'];
const DEFAULT_TAB: TabKey = 'models';

const readTab = (
  param: string | null,
  visible: readonly (typeof TABS)[number][]
): TabKey => visible.find(tab => tab.key === param)?.key ?? DEFAULT_TAB;

const LlmAdminPage = () => {
  const { t } = useTranslation();
  const { canRead, canReadUsage } = useLlmPermissions();
  const [searchParams, setSearchParams] = useSearchParams();
  const visibleTabs = TABS.filter(tab => tab.key !== 'usage' || canReadUsage);
  const activeTab = readTab(searchParams.get('tab'), visibleTabs);

  const handleTabSelect = (key: string | null) => {
    if (!key) return;
    setSearchParams(
      prev => {
        prev.set('tab', readTab(key, visibleTabs));
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
                <Tab.Pane eventKey="models" mountOnEnter unmountOnExit>
                  <ModelsTab />
                </Tab.Pane>
                <Tab.Pane eventKey="credentials" mountOnEnter unmountOnExit>
                  <CredentialsTab />
                </Tab.Pane>
                <Tab.Pane eventKey="accounts" mountOnEnter unmountOnExit>
                  <AccountsTab />
                </Tab.Pane>
                {canReadUsage && (
                  <Tab.Pane eventKey="usage" mountOnEnter unmountOnExit>
                    <UsageTab />
                  </Tab.Pane>
                )}
                <Tab.Pane eventKey="budget" mountOnEnter unmountOnExit>
                  <BudgetTab />
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
