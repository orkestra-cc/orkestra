import { Suspense, lazy } from 'react';
import App from 'App';
import paths, { rootPaths } from './paths';
import { Navigate, createBrowserRouter, RouteObject } from 'react-router';

import MainLayout from '../layouts/MainLayout';
import ErrorLayout from '../layouts/ErrorLayout';
import Landing from 'reference/pages/landing/Landing';
import ProtectedRoute from 'components/authentication/ProtectedRoute';
const Accordion = lazy(() => import('reference/components/ui/Accordion'));
const Alerts = lazy(() => import('reference/components/ui/Alerts'));
const Badges = lazy(() => import('reference/components/ui/Badges'));
const Breadcrumbs = lazy(() => import('reference/components/ui/Breadcrumb'));
const Buttons = lazy(() => import('reference/components/ui/Buttons'));
const CalendarExample = lazy(() => import('reference/components/misc/CalendarExample'));
const Cards = lazy(() => import('reference/components/ui/Cards'));
const Dropdowns = lazy(() => import('reference/components/ui/Dropdowns'));
const ListGroups = lazy(() => import('reference/components/ui/ListGroups'));
const Modals = lazy(() => import('reference/components/ui/Modals'));
const Offcanvas = lazy(() => import('reference/components/ui/Offcanvas'));
const Pagination = lazy(() => import('reference/components/ui/Pagination'));
const BasicProgressBar = lazy(() => import('reference/components/ui/ProgressBar'));
const Spinners = lazy(() => import('reference/components/ui/Spinners'));
const Toasts = lazy(() => import('reference/components/ui/Toasts'));
const Avatar = lazy(() => import('reference/components/ui/Avatar'));
const Image = lazy(() => import('reference/components/media/Image'));
const Tooltips = lazy(() => import('reference/components/ui/Tooltips'));
const Popovers = lazy(() => import('reference/components/ui/Popovers'));
const Figures = lazy(() => import('reference/components/media/Figures'));
const Hoverbox = lazy(() => import('reference/components/ui/Hoverbox'));
const Tables = lazy(() => import('reference/components/tables/Tables'));
const FormControl = lazy(() => import('reference/components/forms/FormControl'));
const InputGroup = lazy(() => import('reference/components/forms/InputGroup'));
const Select = lazy(() => import('reference/components/forms/Select'));
const Checks = lazy(() => import('reference/components/forms/Checks'));
const Range = lazy(() => import('reference/components/forms/Range'));
const FormLayout = lazy(() => import('reference/components/forms/FormLayout'));
const FloatingLabels = lazy(() => import('reference/components/forms/FloatingLabels'));
const FormValidation = lazy(() => import('reference/components/forms/FormValidation'));
const BootstrapCarousel = lazy(
  () => import('reference/components/media/BootstrapCarousel')
);
const SlickCarousel = lazy(() => import('reference/components/media/SlickCarousel'));
const Navs = lazy(() => import('reference/components/navigation/Navs'));
const Navbars = lazy(() => import('reference/components/navigation/Navbars'));
const Tabs = lazy(() => import('reference/components/navigation/Tabs'));
const Collapse = lazy(() => import('reference/components/ui/Collapse'));
const CountUp = lazy(() => import('reference/components/ui/CountUp'));
const Embed = lazy(() => import('reference/components/media/Embed'));
const Backgrounds = lazy(() => import('reference/components/ui/Backgrounds'));
const Search = lazy(() => import('reference/components/ui/Search'));
const VerticalNavbar = lazy(() => import('reference/components/navigation/VerticalNavbar'));
const NavBarTop = lazy(() => import('reference/components/navigation/NavBarTop'));
const NavbarDoubleTop = lazy(() => import('reference/components/navigation/NavbarDoubleTop'));
const ComboNavbar = lazy(() => import('reference/components/navigation/ComboNavbar'));
const TypedText = lazy(() => import('reference/components/ui/TypedText'));
const FileUploader = lazy(() => import('reference/components/forms/FileUploader'));
const Borders = lazy(() => import('reference/utilities/Borders'));
const Colors = lazy(() => import('reference/utilities/Colors'));
const Background = lazy(() => import('reference/utilities/Background'));
const ColoredLinks = lazy(() => import('reference/utilities/ColoredLinks'));
const Display = lazy(() => import('reference/utilities/Display'));
const Visibility = lazy(() => import('reference/utilities/Visibility'));
const StretchedLink = lazy(() => import('reference/utilities/StretchedLink'));
const Float = lazy(() => import('reference/utilities/Float'));
const Position = lazy(() => import('reference/utilities/Position'));
const Spacing = lazy(() => import('reference/utilities/Spacing'));
const Sizing = lazy(() => import('reference/utilities/Sizing'));
const TextTruncation = lazy(() => import('reference/utilities/TextTruncation'));
const Typography = lazy(() => import('reference/utilities/Typography'));
const VerticalAlign = lazy(() => import('reference/utilities/VerticalAlign'));
const Flex = lazy(() => import('reference/utilities/Flex'));
const Grid = lazy(() => import('reference/utilities/Grid'));
const WizardForms = lazy(() => import('reference/components/forms/WizardForms'));
const GettingStarted = lazy(() => import('reference/documentation/GettingStarted'));
const Configuration = lazy(() => import('reference/documentation/Configuration'));
const DarkMode = lazy(() => import('reference/documentation/DarkMode'));
const Plugins = lazy(() => import('reference/documentation/Plugins'));
const Styling = lazy(() => import('reference/documentation/Styling'));
const DesignFile = lazy(() => import('reference/documentation/DesignFile'));
const Starter = lazy(() => import('reference/pages/Starter'));
const AnimatedIcons = lazy(() => import('reference/components/icons/AnimatedIcons'));
const DatePicker = lazy(() => import('reference/components/forms/DatePicker'));
const FontAwesome = lazy(() => import('reference/components/icons/FontAwesome'));
const Changelog = lazy(() => import('reference/documentation/change-log/ChangeLog'));
const Analytics = lazy(() => import('reference/dashboards/AnalyticsDashboard'));
const Crm = lazy(() => import('reference/dashboards/CrmDashboard'));
const Saas = lazy(() => import('reference/dashboards/SaasDashboard'));
const Profile = lazy(() => import('pages/operator/profile/OperatorProfile'));
const Associations = lazy(() => import('reference/pages/associations/Associations'));
const Followers = lazy(() => import('reference/app-examples/social/followers/Followers'));
const Notifications = lazy(
  () => import('reference/app-examples/social/notifications/Notifications')
);
const ActivityLog = lazy(
  () => import('reference/app-examples/social/activity-log/ActivityLog')
);
const Settings = lazy(() => import('pages/user/settings/Settings'));
const Feed = lazy(() => import('reference/app-examples/social/feed/Feed'));
const Placeholder = lazy(() => import('reference/components/ui/Placeholder'));
const Lightbox = lazy(() => import('reference/components/media/Lightbox'));
const Calendar = lazy(() => import('reference/app-examples/calendar/Calendar'));
import FaqAlt from 'reference/pages/faq/faq-alt/FaqAlt';
import FaqBasic from 'reference/pages/faq/faq-basic/FaqBasic';
import FaqAccordion from 'reference/pages/faq/faq-accordion/FaqAccordion';
import PrivacyPolicy from 'reference/pages/miscellaneous/privacy-policy/PrivacyPolicy';
import InvitePeople from 'reference/pages/miscellaneous/invite-people/InvitePeople';
import AuthTestPage from 'reference/test/AuthTestPage';
import RoleNavigationTester from 'reference/test/RoleNavigationTester';
import PricingDefault from 'reference/pages/pricing/pricing-default/PricingDefault';
import PricingAlt from 'reference/pages/pricing/pricing-alt/PricingAlt';
import CreateEvent from 'reference/app-examples/events/create-an-event/CreateEvent';
import EventList from 'reference/app-examples/events/event-list/EventList';
import EventDetail from 'reference/app-examples/events/event-detail/EventDetail';
import EmailDetail from 'reference/app-examples/email/email-detail/EmailDetail';
import Compose from 'reference/app-examples/email/compose/Compose';
import Inbox from 'reference/app-examples/email/inbox/Inbox';
import Rating from 'reference/components/forms/Rating';
import AdvanceSelect from 'reference/components/forms/AdvanceSelect';
import Editor from 'reference/components/forms/Editor';
import Chat from 'reference/app-examples/chat/Chat';
const Kanban = lazy(() => import('reference/app-examples/kanban/Kanban'));
import DraggableExample from 'reference/components/misc/DraggableExample';
const LineCharts = lazy(
  () => import('reference/charts/echarts/line-charts')
);
const BarCharts = lazy(
  () => import('reference/charts/echarts/bar-charts')
);
const CandlestickCharts = lazy(
  () => import('reference/charts/echarts/candlestick-charts')
);
const GeoMaps = lazy(
  () => import('reference/charts/echarts/geo-map')
);
const ScatterCharts = lazy(
  () => import('reference/charts/echarts/scatter-charts')
);
const PieCharts = lazy(
  () => import('reference/charts/echarts/pie-charts')
);
const RadarCharts = lazy(
  () => import('reference/charts/echarts/radar-charts/Index')
);
const HeatmapCharts = lazy(
  () => import('reference/charts/echarts/heatmap-chart')
);
const Chartjs = lazy(() => import('reference/charts/chartjs'));
const D3js = lazy(() => import('reference/charts/d3js'));
import HowToUse from 'reference/charts/echarts/HowToUse';
const GoogleMapExample = lazy(() => import('reference/components/maps/GoogleMapExample'));
import LeafletMapExample from 'reference/components/maps/LeafletMapExample';
import CookieNoticeExample from 'reference/components/misc/CookieNoticeExample';
import Scrollbar from 'reference/components/misc/Scrollbar';
import Scrollspy from 'reference/components/misc/Scrollspy';
import ReactIcons from 'reference/components/icons/ReactIcons';
import ReactPlayerExample from 'reference/components/media/ReactPlayerExample';
import EmojiPickerExample from 'reference/components/forms/EmojiPicker';
import TreeviewExample from 'reference/components/ui/TreeviewExample';
import Timeline from 'reference/components/ui/Timeline';
const Widgets = lazy(() => import('reference/components/widgets/Widgets'));
const ProjectManagement = lazy(
  () => import('reference/dashboards/ProjectManagementDashboard')
);
const Migration = lazy(() => import('reference/documentation/migration/Migration'));

import Error401 from 'components/errors/Error401';
import Error404 from 'components/errors/Error404';
import Error500 from 'components/errors/Error500';

import Login from 'components/authentication/Login';
import Register from 'components/authentication/Register';
import ForgotPassword from 'components/authentication/ForgotPassword';
import ResetPassword from 'components/authentication/ResetPassword';
import VerifyEmailPage from 'components/authentication/VerifyEmailPage';
import SocialAuthCallback from 'components/authentication/SocialAuthCallback';
const Dashboard = lazy(() => import('reference/dashboards/DefaultDashboard'));
import Faq from 'reference/documentation/Faq';
const SupportDesk = lazy(() => import('reference/dashboards/SupportDeskDashboard'));
const UserManagement = lazy(() => import('pages/admin/users'));
const ModuleManagement = lazy(() => import('pages/admin/modules'));
const RoleManagement = lazy(() => import('pages/admin/roles'));
const AdminUserProfile = lazy(
  () => import('pages/admin/user-profile/AdminUserProfile')
);

const AdminSettings = lazy(
  () => import('pages/admin/settings/AdminSettings')
);
const CompanyManagement = lazy(
  () => import('pages/admin/settings/companies')
);
const UserDashboard = lazy(
  () => import('pages/user/dashboard/UserDashboard')
);
const UserCalendar = lazy(
  () => import('pages/user/calendar/UserCalendar')
);
const OperatorProfile = lazy(
  () => import('pages/operator/profile/OperatorProfile')
);

// Document templates
const DocumentTemplates = lazy(
  () => import('pages/admin/templates')
);

// Billing pages
const BillingDashboard = lazy(
  () => import('pages/billing/dashboard')
);
const CustomerManagement = lazy(
  () => import('pages/billing/customers')
);
const SupplierManagement = lazy(
  () => import('pages/billing/suppliers')
);
const IssuedInvoices = lazy(
  () => import('pages/billing/invoices/issued')
);
const ReceivedInvoices = lazy(
  () => import('pages/billing/invoices/received')
);
const SDINotifications = lazy(
  () => import('pages/billing/notifications')
);
const NewIssuedInvoice = lazy(
  () => import('pages/billing/invoices/issued/NewIssuedInvoice')
);
const IssuedInvoiceDetail = lazy(
  () => import('pages/billing/invoices/issued/IssuedInvoiceDetail')
);
const ReceivedInvoiceDetail = lazy(
  () => import('pages/billing/invoices/received/ReceivedInvoiceDetail')
);

// Company pages
const CompanyLookup = lazy(
  () => import('pages/company/lookup')
);
const CompanyDetail = lazy(
  () => import('pages/company/lookup/CompanyDetail')
);
const CompanySearch = lazy(
  () => import('pages/company/search')
);

// Graph module lazy imports
const GraphExplorer = lazy(() => import('pages/graph/explorer'));
const GraphAlgorithms = lazy(() => import('pages/graph/algorithms'));
const GraphDatabases = lazy(() => import('pages/graph/databases'));
const GraphVector = lazy(() => import('pages/graph/vector'));
const GraphDocuments = lazy(() => import('pages/graph/documents'));
const GraphRelationships = lazy(() => import('pages/graph/relationships'));
const GraphRAG = lazy(() => import('pages/graph/rag'));

// AI module lazy imports
const AIModels = lazy(() => import('pages/ai/models'));
const AgentProjects = lazy(() => import('pages/ai/agents'));
const AgentChat = lazy(() => import('pages/ai/agents/AgentChat'));
const PersonalAgentChat = lazy(() => import('pages/ai/personal-agent/PersonalAgentChat'));

// Sales Intelligence pages
const SalesProspect = lazy(() => import('pages/sales/prospect'));
const SalesSkill = lazy(() => import('pages/sales/skills'));
const SalesJobs = lazy(() => import('pages/sales/jobs'));
const SalesJobDetail = lazy(() => import('pages/sales/jobs/detail'));
const SalesReports = lazy(() => import('pages/sales/reports'));
const SalesReportDetail = lazy(() => import('pages/sales/reports/detail'));
const SalesSettings = lazy(() => import('pages/sales/settings'));

import TableView from 'reference/app-examples/support-desk/tickets-layout/TableView';
import CardView from 'reference/app-examples/support-desk/tickets-layout/CardView';
import Contacts from 'reference/app-examples/support-desk/contacts/Contacts';
import ContactDetails from 'reference/app-examples/support-desk/contact-details/ContactDetails';
import TicketsPreview from 'reference/app-examples/support-desk/tickets-preview/TicketsPreview';
import QuickLinks from 'reference/app-examples/support-desk/quick-links/QuickLinks';
import Reports from 'reference/app-examples/support-desk/reports/Reports';
import InputMaskExample from 'reference/components/forms/InputMaskExample';
import RangeSlider from 'reference/components/forms/RangeSlider';
import VerticalNavLayout from 'layouts/VerticalNavLayout';
import TopNavLayout from 'layouts/TopNavLayout';
import ComboNavLayout from 'layouts/ComboNavLayout';
import DoubleTopNavLayout from 'layouts/DoubleTopNavLayout';
import FalconLoader from 'components/common/FalconLoader';

const routes: RouteObject[] = [
  {
    element: <App />,
    children: [
      {
        path: 'landing',
        element: <Landing />
      },
      {
        path: rootPaths.errorsRoot,
        element: <ErrorLayout />,
        children: [
          {
            path: paths.error401,
            element: <Error401 />
          },
          {
            path: paths.error404,
            element: <Error404 />
          },
          {
            path: paths.error500,
            element: <Error500 />
          }
        ]
      },
      {
        path: 'login',
        element: <Login />
      },
      {
        path: 'register',
        element: <Register />
      },
      {
        path: 'forgot-password',
        element: <ForgotPassword />
      },
      {
        path: 'reset-password',
        element: <ResetPassword />
      },
      {
        path: 'verify-email',
        element: <VerifyEmailPage />
      },
      {
        path: 'auth/callback',
        element: <SocialAuthCallback />
      },
      {
        path: '/',
        element: (
          <ProtectedRoute>
            <MainLayout />
          </ProtectedRoute>
        ),
        children: [
          {
            index: true,
            element: <Navigate to="/dashboard/analytics" replace />
          },
          {
            path: rootPaths.dashboardRoot,
            children: [
              {
                path: paths.analytics,
                element: (
                  <Suspense
                    key="dashboard-analytics"
                    fallback={<FalconLoader />}
                  >
                    <Analytics />
                  </Suspense>
                )
              },
              {
                path: paths.crm,
                element: (
                  <Suspense key="dashboard-crm" fallback={<FalconLoader />}>
                    <Crm />
                  </Suspense>
                )
              },
              {
                path: paths.saas,
                element: (
                  <Suspense key="dashboard-sass" fallback={<FalconLoader />}>
                    <Saas />
                  </Suspense>
                )
              },
              {
                path: paths.projectManagement,
                element: (
                  <Suspense
                    key="dashboard-projectManagement"
                    fallback={<FalconLoader />}
                  >
                    <ProjectManagement />
                  </Suspense>
                )
              },
              {
                path: paths.supportDesk,
                element: (
                  <Suspense
                    key="dashboard-supportDesk"
                    fallback={<FalconLoader />}
                  >
                    <SupportDesk />
                  </Suspense>
                )
              }
            ]
          },
          {
            path: rootPaths.appsRoot,
            children: [
              {
                path: paths.calendar,
                element: (
                  <Suspense key="calendar" fallback={<FalconLoader />}>
                    <Calendar />
                  </Suspense>
                )
              },
              {
                path: paths.chat,
                element: <Chat />
              },
              {
                path: paths.kanban,
                element: (
                  <Suspense key="kanban" fallback={<FalconLoader />}>
                    <Kanban />
                  </Suspense>
                )
              }
            ]
          },
          {
            path: rootPaths.emailRoot,
            children: [
              {
                path: paths.emailInbox,
                element: <Inbox />
              },
              {
                path: paths.emailDetail,
                element: <EmailDetail />
              },
              {
                path: paths.emailCompose,
                element: <Compose />
              }
            ]
          },
          {
            path: rootPaths.eventsRoot,
            children: [
              {
                path: paths.createEvent,
                element: <CreateEvent />
              },
              {
                path: paths.eventDetail,
                element: <EventDetail />
              },
              {
                path: paths.eventList,
                element: <EventList />
              }
            ]
          },
          {
            path: rootPaths.socialRoot,
            children: [
              {
                path: paths.feed,
                element: <Feed />
              },
              {
                path: paths.activityLog,
                element: <ActivityLog />
              },
              {
                path: paths.notifications,
                element: <Notifications />
              },
              {
                path: paths.followers,
                element: <Followers />
              }
            ]
          },
          {
            path: rootPaths.supportDeskRoot,
            children: [
              {
                path: paths.ticketsTable,
                element: <TableView />
              },
              {
                path: paths.ticketsCard,
                element: <CardView />
              },
              {
                path: paths.contacts,
                element: <Contacts />
              },
              {
                path: paths.contactDetails,
                element: <ContactDetails />
              },
              {
                path: paths.ticketsPreview,
                element: <TicketsPreview />
              },
              {
                path: paths.quickLinks,
                element: <QuickLinks />
              },
              {
                path: paths.reports,
                element: <Reports />
              }
            ]
          },
          {
            path: rootPaths.pagesRoot,
            children: [
              {
                path: paths.starter,
                element: <Starter />
              }
            ]
          },
          {
            path: rootPaths.userRoot,
            children: [
              {
                path: paths.userProfile,
                element: <Profile />
              },
              {
                path: paths.userSettings,
                element: <Settings />
              }
            ]
          },
          {
            path: rootPaths.pricingRoot,
            children: [
              {
                path: paths.pricingDefault,
                element: <PricingDefault />
              },
              {
                path: paths.pricingAlt,
                element: <PricingAlt />
              }
            ]
          },
          {
            path: rootPaths.faqRoot,
            children: [
              {
                path: paths.faqBasic,
                element: <FaqBasic />
              },
              {
                path: paths.faqAlt,
                element: <FaqAlt />
              },
              {
                path: paths.faqAccordion,
                element: <FaqAccordion />
              }
            ]
          },
          {
            path: rootPaths.miscRoot,
            children: [
              {
                path: paths.associations,
                element: <Associations />
              },
              {
                path: paths.invitePeople,
                element: <InvitePeople />
              },
              {
                path: paths.privacyPolicy,
                element: <PrivacyPolicy />
              },
              {
                path: paths.authTest,
                element: <AuthTestPage />
              }
            ]
          },
          {
            path: rootPaths.adminRoot,
            children: [
              {
                path: paths.userManagement,
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator']
                    ]}
                  >
                    <Suspense
                      key="admin-userManagement"
                      fallback={<FalconLoader />}
                    >
                      <UserManagement />
                    </Suspense>
                  </ProtectedRoute>
                )
              },
              {
                path: 'modules',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator']
                    ]}
                  >
                    <Suspense
                      key="admin-modules"
                      fallback={<FalconLoader />}
                    >
                      <ModuleManagement />
                    </Suspense>
                  </ProtectedRoute>
                )
              },
              {
                path: 'roles',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['authz.role.read', 'developer', 'ceo', 'administrator']
                    ]}
                  >
                    <Suspense
                      key="admin-roles"
                      fallback={<FalconLoader />}
                    >
                      <RoleManagement />
                    </Suspense>
                  </ProtectedRoute>
                )
              },
              {
                path: 'user/profile/:userId',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator']
                    ]}
                  >
                    <Suspense
                      key="admin-userProfile"
                      fallback={<FalconLoader />}
                    >
                      <AdminUserProfile />
                    </Suspense>
                  </ProtectedRoute>
                )
              },
              {
                path: 'settings',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator']
                    ]}
                  >
                    <Suspense
                      key="admin-settings"
                      fallback={<FalconLoader />}
                    >
                      <AdminSettings />
                    </Suspense>
                  </ProtectedRoute>
                )
              },
              {
                path: 'settings/companies',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator']
                    ]}
                  >
                    <Suspense
                      key="admin-companies"
                      fallback={<FalconLoader />}
                    >
                      <CompanyManagement />
                    </Suspense>
                  </ProtectedRoute>
                )
              },
              {
                path: 'templates',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator', 'manager']
                    ]}
                  >
                    <Suspense
                      key="admin-templates"
                      fallback={<FalconLoader />}
                    >
                      <DocumentTemplates />
                    </Suspense>
                  </ProtectedRoute>
                )
              }
            ]
          },
          {
            path: rootPaths.userRoot,
            children: [
              {
                path: 'profile',
                element: (
                  <Suspense key="operator-profile" fallback={<FalconLoader />}>
                    <OperatorProfile />
                  </Suspense>
                )
              },
              {
                path: 'dashboard',
                element: (
                  <Suspense key="user-dashboard" fallback={<FalconLoader />}>
                    <UserDashboard />
                  </Suspense>
                )
              },
              {
                path: 'calendar',
                element: (
                  <Suspense key="user-calendar" fallback={<FalconLoader />}>
                    <UserCalendar />
                  </Suspense>
                )
              }
            ]
          },
          // Billing routes
          {
            path: 'billing/dashboard',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-dashboard"
                  fallback={<FalconLoader />}
                >
                  <BillingDashboard />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/customers',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-customers"
                  fallback={<FalconLoader />}
                >
                  <CustomerManagement />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/suppliers',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-suppliers"
                  fallback={<FalconLoader />}
                >
                  <SupplierManagement />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/invoices/issued/new',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-invoices-issued-new"
                  fallback={<FalconLoader />}
                >
                  <NewIssuedInvoice />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/invoices/issued/:invoiceId',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-invoices-issued-detail"
                  fallback={<FalconLoader />}
                >
                  <IssuedInvoiceDetail />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/invoices/issued',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-invoices-issued"
                  fallback={<FalconLoader />}
                >
                  <IssuedInvoices />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/invoices/received',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-invoices-received"
                  fallback={<FalconLoader />}
                >
                  <ReceivedInvoices />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/invoices/received/:invoiceId',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-invoices-received-detail"
                  fallback={<FalconLoader />}
                >
                  <ReceivedInvoiceDetail />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'billing/notifications',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="billing-notifications"
                  fallback={<FalconLoader />}
                >
                  <SDINotifications />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'company/lookup',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator', 'manager']
                ]}
              >
                <Suspense
                  key="company-lookup"
                  fallback={<FalconLoader />}
                >
                  <CompanyLookup />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'company/lookup/:companyId',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator', 'manager']
                ]}
              >
                <Suspense
                  key="company-detail"
                  fallback={<FalconLoader />}
                >
                  <CompanyDetail />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'company/search',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator', 'manager']
                ]}
              >
                <Suspense
                  key="company-search"
                  fallback={<FalconLoader />}
                >
                  <CompanySearch />
                </Suspense>
              </ProtectedRoute>
            )
          },
          // Graph Database routes
          {
            path: 'graph/explorer',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-explorer" fallback={<FalconLoader />}>
                  <GraphExplorer />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'graph/algorithms',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-algorithms" fallback={<FalconLoader />}>
                  <GraphAlgorithms />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'graph/databases',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-databases" fallback={<FalconLoader />}>
                  <GraphDatabases />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'graph/vector',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-vector" fallback={<FalconLoader />}>
                  <GraphVector />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'graph/models',
            element: <Navigate to="/ai/models" replace />
          },
          {
            path: 'graph/documents',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-documents" fallback={<FalconLoader />}>
                  <GraphDocuments />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'graph/relationships',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-relationships" fallback={<FalconLoader />}>
                  <GraphRelationships />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'graph/rag',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="graph-rag" fallback={<FalconLoader />}>
                  <GraphRAG />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'ai/models',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator']]}
              >
                <Suspense key="ai-models" fallback={<FalconLoader />}>
                  <AIModels />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'ai/personal-agent',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator', 'manager', 'operator', 'guest']]}
              >
                <Suspense key="ai-personal-agent" fallback={<FalconLoader />}>
                  <PersonalAgentChat />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'ai/agents',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}
              >
                <Suspense key="ai-agents" fallback={<FalconLoader />}>
                  <AgentProjects />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'ai/agents/:uuid/chat',
            element: (
              <ProtectedRoute
                requiredPermissions={[['developer', 'ceo', 'administrator', 'manager', 'operator']]}
              >
                <Suspense key="ai-agent-chat" fallback={<FalconLoader />}>
                  <AgentChat />
                </Suspense>
              </ProtectedRoute>
            )
          },

          // ── Sales Intelligence ──
          {
            path: 'sales/prospect',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-prospect" fallback={<FalconLoader />}>
                  <SalesProspect />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'sales/skills/:skill',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-skill" fallback={<FalconLoader />}>
                  <SalesSkill />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'sales/jobs',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-jobs" fallback={<FalconLoader />}>
                  <SalesJobs />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'sales/jobs/:uuid',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-job-detail" fallback={<FalconLoader />}>
                  <SalesJobDetail />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'sales/reports',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-reports" fallback={<FalconLoader />}>
                  <SalesReports />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'sales/reports/:uuid',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-report-detail" fallback={<FalconLoader />}>
                  <SalesReportDetail />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'sales/settings',
            element: (
              <ProtectedRoute requiredPermissions={[['developer', 'ceo', 'administrator', 'manager']]}>
                <Suspense key="sales-settings" fallback={<FalconLoader />}>
                  <SalesSettings />
                </Suspense>
              </ProtectedRoute>
            )
          },

          {
            path: rootPaths.formsRoot,
            children: [
              {
                path: rootPaths.basicFormsRoot,
                children: [
                  {
                    path: paths.formControl,
                    element: <FormControl />
                  },
                  {
                    path: paths.inputGroup,
                    element: <InputGroup />
                  },
                  {
                    path: paths.select,
                    element: <Select />
                  },
                  {
                    path: paths.checks,
                    element: <Checks />
                  },
                  {
                    path: paths.range,
                    element: <Range />
                  },
                  {
                    path: paths.formLayout,
                    element: <FormLayout />
                  }
                ]
              },
              {
                path: rootPaths.advanceFormsRoot,
                children: [
                  {
                    path: paths.advanceSelect,
                    element: <AdvanceSelect />
                  },
                  {
                    path: paths.datePicker,
                    element: <DatePicker />
                  },
                  {
                    path: paths.editor,
                    element: <Editor />
                  },
                  {
                    path: paths.emojiButton,
                    element: <EmojiPickerExample />
                  },
                  {
                    path: paths.fileUploader,
                    element: <FileUploader />
                  },
                  {
                    path: paths.inputMask,
                    element: <InputMaskExample />
                  },
                  {
                    path: paths.rangeSlider,
                    element: <RangeSlider />
                  },
                  {
                    path: paths.rating,
                    element: <Rating />
                  }
                ]
              },
              {
                path: paths.floatingLabels,
                element: <FloatingLabels />
              },
              {
                path: paths.wizard,
                element: <WizardForms />
              },
              {
                path: paths.validation,
                element: <FormValidation />
              }
            ]
          },
          {
            path: rootPaths.tableRoot,
            element: (
              <Suspense key="tables" fallback={<FalconLoader />}>
                <Tables />
              </Suspense>
            )
          },
          {
            path: rootPaths.chartsRoot,
            children: [
              {
                path: paths.chartjs,
                element: (
                  <Suspense key="chartjs" fallback={<FalconLoader />}>
                    <Chartjs />
                  </Suspense>
                )
              },
              {
                path: paths.d3js,
                element: (
                  <Suspense key="d3j" fallback={<FalconLoader />}>
                    <D3js />
                  </Suspense>
                )
              },
              {
                path: rootPaths.echartsRoot,
                children: [
                  {
                    path: paths.echartsHowToUse,
                    element: <HowToUse />
                  },
                  {
                    path: paths.lineCharts,
                    element: (
                      <Suspense
                        key="echarts-lineChart"
                        fallback={<FalconLoader />}
                      >
                        <LineCharts />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.barCharts,
                    element: (
                      <Suspense
                        key="echarts-barChart"
                        fallback={<FalconLoader />}
                      >
                        <BarCharts />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.candlestickCharts,
                    element: (
                      <Suspense
                        key="echarts-candleStick"
                        fallback={<FalconLoader />}
                      >
                        <CandlestickCharts />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.geoMap,
                    element: (
                      <Suspense
                        key="echarts-geoMap"
                        fallback={<FalconLoader />}
                      >
                        <GeoMaps />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.scatterCharts,
                    element: (
                      <Suspense
                        key="echarts-scatterChart"
                        fallback={<FalconLoader />}
                      >
                        <ScatterCharts />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.pieCharts,
                    element: (
                      <Suspense
                        key="echarts-pieChart"
                        fallback={<FalconLoader />}
                      >
                        <PieCharts />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.radarCharts,
                    element: (
                      <Suspense
                        key="echarts-radarChart"
                        fallback={<FalconLoader />}
                      >
                        <RadarCharts />
                      </Suspense>
                    )
                  },
                  {
                    path: paths.heatmapCharts,
                    element: (
                      <Suspense
                        key="echarts-heatmapChart"
                        fallback={<FalconLoader />}
                      >
                        <HeatmapCharts />
                      </Suspense>
                    )
                  }
                ]
              }
            ]
          },
          {
            path: rootPaths.iconsRoot,
            children: [
              {
                path: paths.fontAwesome,
                element: <FontAwesome />
              },
              {
                path: paths.reactIcons,
                element: <ReactIcons />
              }
            ]
          },
          {
            path: rootPaths.mapsRoot,
            children: [
              {
                path: paths.googleMap,
                element: (
                  <Suspense key="googleMap" fallback={<FalconLoader />}>
                    <GoogleMapExample />
                  </Suspense>
                )
              },
              {
                path: paths.leafletMap,
                element: (
                  <Suspense key="leafletMap" fallback={<FalconLoader />}>
                    <LeafletMapExample />
                  </Suspense>
                )
              }
            ]
          },
          {
            path: rootPaths.componentsRoot,
            children: [
              {
                path: paths.alerts,
                element: (
                  <Suspense key="alerts" fallback={<FalconLoader />}>
                    <Alerts />
                  </Suspense>
                )
              },
              {
                path: paths.accordion,
                element: (
                  <Suspense key="accordion" fallback={<FalconLoader />}>
                    <Accordion />
                  </Suspense>
                )
              },
              {
                path: paths.animatedIcons,
                element: <AnimatedIcons />
              },
              {
                path: paths.background,
                element: <Backgrounds />
              },
              {
                path: paths.badges,
                element: (
                  <Suspense key="badges" fallback={<FalconLoader />}>
                    <Badges />
                  </Suspense>
                )
              },
              {
                path: paths.breadcrumbs,
                element: (
                  <Suspense key="breadcrumbs" fallback={<FalconLoader />}>
                    <Breadcrumbs />
                  </Suspense>
                )
              },
              {
                path: paths.buttons,
                element: (
                  <Suspense key="buttons" fallback={<FalconLoader />}>
                    <Buttons />
                  </Suspense>
                )
              },
              {
                path: paths.calendarExample,
                element: (
                  <Suspense key="calendarExample" fallback={<FalconLoader />}>
                    <CalendarExample />
                  </Suspense>
                )
              },
              {
                path: paths.cards,
                element: <Cards />
              },
              {
                path: paths.cards,
                element: <Cards />
              },
              {
                path: rootPaths.carouselRoot,
                children: [
                  {
                    path: paths.bootstrapCarousel,
                    element: <BootstrapCarousel />
                  },
                  {
                    path: paths.slickCarousel,
                    element: <SlickCarousel />
                  }
                ]
              },
              {
                path: paths.collapse,
                element: <Collapse />
              },
              {
                path: paths.cookieNotice,
                element: <CookieNoticeExample />
              },
              {
                path: paths.countup,
                element: <CountUp />
              },
              {
                path: paths.draggable,
                element: <DraggableExample />
              },
              {
                path: paths.dropdowns,
                element: <Dropdowns />
              },
              {
                path: paths.listGroup,
                element: <ListGroups />
              },
              {
                path: paths.modals,
                element: <Modals />
              },
              {
                path: paths.offcanvas,
                element: <Offcanvas />
              },
              {
                path: rootPaths.navsAndTabsRoot,
                children: [
                  {
                    path: paths.navs,
                    element: <Navs />
                  },
                  {
                    path: paths.navbar,
                    element: <Navbars />
                  },
                  {
                    path: paths.verticalNavbar,
                    element: <VerticalNavbar />
                  },
                  {
                    path: paths.topNavbar,
                    element: <NavBarTop />
                  },
                  {
                    path: paths.doubleTopNavbar,
                    element: <NavbarDoubleTop />
                  },
                  {
                    path: paths.comboNavbar,
                    element: <ComboNavbar />
                  },
                  {
                    path: paths.tabs,
                    element: <Tabs />
                  }
                ]
              },
              {
                path: rootPaths.picturesRoot,
                children: [
                  {
                    path: paths.avatar,
                    element: <Avatar />
                  },
                  {
                    path: paths.images,
                    element: <Image />
                  },
                  {
                    path: paths.figures,
                    element: <Figures />
                  },
                  {
                    path: paths.hoverbox,
                    element: <Hoverbox />
                  },
                  {
                    path: paths.lightbox,
                    element: <Lightbox />
                  }
                ]
              },
              {
                path: paths.progressBar,
                element: <BasicProgressBar />
              },
              {
                path: paths.pagination,
                element: <Pagination />
              },
              {
                path: paths.placeholder,
                element: <Placeholder />
              },
              {
                path: paths.popovers,
                element: <Popovers />
              },
              {
                path: paths.scrollspy,
                element: <Scrollspy />
              },
              {
                path: paths.search,
                element: <Search />
              },
              {
                path: paths.spinners,
                element: <Spinners />
              },
              {
                path: paths.timeline,
                element: <Timeline />
              },
              {
                path: paths.toasts,
                element: <Toasts />
              },
              {
                path: paths.tooltips,
                element: <Tooltips />
              },
              {
                path: paths.treeview,
                element: <TreeviewExample />
              },
              {
                path: paths.typedText,
                element: <TypedText />
              },
              {
                path: rootPaths.videosRoot,
                children: [
                  {
                    path: paths.embedVideo,
                    element: <Embed />
                  },
                  {
                    path: paths.reactPlayer,
                    element: <ReactPlayerExample />
                  }
                ]
              }
            ]
          },
          {
            path: rootPaths.utilitiesRoot,
            children: [
              {
                path: paths.backgroundColor,
                element: <Background />
              },
              {
                path: paths.borders,
                element: <Borders />
              },
              {
                path: paths.colors,
                element: <Colors />
              },
              {
                path: paths.coloredLinks,
                element: <ColoredLinks />
              },
              {
                path: paths.display,
                element: <Display />
              },
              {
                path: paths.flex,
                element: <Flex />
              },
              {
                path: paths.float,
                element: <Float />
              },
              {
                path: paths.grid,
                element: <Grid />
              },
              {
                path: paths.scrollBar,
                element: <Scrollbar />
              },
              {
                path: paths.position,
                element: <Position />
              },
              {
                path: paths.spacing,
                element: <Spacing />
              },
              {
                path: paths.sizing,
                element: <Sizing />
              },
              {
                path: paths.stretchedLink,
                element: <StretchedLink />
              },
              {
                path: paths.textTruncation,
                element: <TextTruncation />
              },
              {
                path: paths.typography,
                element: <Typography />
              },
              {
                path: paths.verticalAlign,
                element: <VerticalAlign />
              },
              {
                path: paths.visibility,
                element: <Visibility />
              }
            ]
          },
          {
            path: rootPaths.docRoot,
            children: [
              {
                path: paths.gettingStarted,
                element: <GettingStarted />
              },
              {
                path: paths.configuration,
                element: <Configuration />
              },
              {
                path: paths.styling,
                element: <Styling />
              },
              {
                path: paths.darkMode,
                element: <DarkMode />
              },
              {
                path: paths.plugin,
                element: <Plugins />
              },
              {
                path: paths.faq,
                element: <Faq />
              },
              {
                path: paths.designFile,
                element: <DesignFile />
              }
            ]
          },
          {
            path: paths.widgets,
            element: (
              <Suspense key="widgets" fallback={<FalconLoader />}>
                <Widgets />
              </Suspense>
            )
          },
          {
            path: paths.changelog,
            element: <Changelog />
          },
          {
            path: paths.migration,
            element: <Migration />
          },
          // Reference Routes (developer-only)
          {
            path: rootPaths.referenceRoot,
            children: [
              // Reference Dashboards
              {
                path: 'dashboards',
                children: [
                  {
                    path: 'default',
                    element: (
                      <Suspense key="ref-dashboard-default" fallback={<FalconLoader />}>
                        <Dashboard />
                      </Suspense>
                    )
                  },
                  {
                    path: 'analytics',
                    element: (
                      <Suspense key="ref-dashboard-analytics" fallback={<FalconLoader />}>
                        <Analytics />
                      </Suspense>
                    )
                  },
                  {
                    path: 'crm',
                    element: (
                      <Suspense key="ref-dashboard-crm" fallback={<FalconLoader />}>
                        <Crm />
                      </Suspense>
                    )
                  },
                  {
                    path: 'saas',
                    element: (
                      <Suspense key="ref-dashboard-saas" fallback={<FalconLoader />}>
                        <Saas />
                      </Suspense>
                    )
                  },
                  {
                    path: 'project-management',
                    element: (
                      <Suspense key="ref-dashboard-pm" fallback={<FalconLoader />}>
                        <ProjectManagement />
                      </Suspense>
                    )
                  },
                  {
                    path: 'support-desk',
                    element: (
                      <Suspense key="ref-dashboard-supportdesk" fallback={<FalconLoader />}>
                        <SupportDesk />
                      </Suspense>
                    )
                  }
                ]
              },
              // Reference App Examples
              {
                path: 'app-examples',
                children: [
                  {
                    path: 'calendar',
                    element: (
                      <Suspense key="ref-app-calendar" fallback={<FalconLoader />}>
                        <Calendar />
                      </Suspense>
                    )
                  },
                  { path: 'chat', element: <Chat /> },
                  {
                    path: 'kanban',
                    element: (
                      <Suspense key="ref-app-kanban" fallback={<FalconLoader />}>
                        <Kanban />
                      </Suspense>
                    )
                  },
                  {
                    path: 'email',
                    children: [
                      { path: 'inbox', element: <Inbox /> },
                      { path: 'compose', element: <Compose /> },
                      { path: 'detail', element: <EmailDetail /> }
                    ]
                  },
                  {
                    path: 'events',
                    children: [
                      { path: 'create', element: <CreateEvent /> },
                      { path: 'list', element: <EventList /> },
                      { path: 'detail', element: <EventDetail /> }
                    ]
                  },
                  {
                    path: 'social',
                    children: [
                      { path: 'feed', element: <Feed /> },
                      { path: 'activity-log', element: <ActivityLog /> },
                      { path: 'notifications', element: <Notifications /> },
                      { path: 'followers', element: <Followers /> }
                    ]
                  },
                  {
                    path: 'support-desk',
                    children: [
                      { path: 'table-view', element: <TableView /> },
                      { path: 'card-view', element: <CardView /> },
                      { path: 'contacts', element: <Contacts /> },
                      { path: 'contact-details', element: <ContactDetails /> },
                      { path: 'tickets-preview', element: <TicketsPreview /> },
                      { path: 'quick-links', element: <QuickLinks /> },
                      { path: 'reports', element: <Reports /> }
                    ]
                  }
                ]
              },
              // Reference Components
              {
                path: 'components',
                children: [
                  { path: 'alerts', element: <Suspense key="ref-comp-alerts" fallback={<FalconLoader />}><Alerts /></Suspense> },
                  { path: 'accordion', element: <Suspense key="ref-comp-accordion" fallback={<FalconLoader />}><Accordion /></Suspense> },
                  { path: 'animated-icons', element: <AnimatedIcons /> },
                  { path: 'backgrounds', element: <Backgrounds /> },
                  { path: 'badges', element: <Suspense key="ref-comp-badges" fallback={<FalconLoader />}><Badges /></Suspense> },
                  { path: 'breadcrumbs', element: <Suspense key="ref-comp-breadcrumbs" fallback={<FalconLoader />}><Breadcrumbs /></Suspense> },
                  { path: 'buttons', element: <Suspense key="ref-comp-buttons" fallback={<FalconLoader />}><Buttons /></Suspense> },
                  { path: 'calendar', element: <Suspense key="ref-comp-calendar" fallback={<FalconLoader />}><CalendarExample /></Suspense> },
                  { path: 'cards', element: <Cards /> },
                  {
                    path: 'carousel',
                    children: [
                      { path: 'bootstrap', element: <BootstrapCarousel /> },
                      { path: 'slick', element: <SlickCarousel /> }
                    ]
                  },
                  { path: 'collapse', element: <Collapse /> },
                  { path: 'cookie-notice', element: <CookieNoticeExample /> },
                  { path: 'countup', element: <CountUp /> },
                  { path: 'draggable', element: <DraggableExample /> },
                  { path: 'dropdowns', element: <Dropdowns /> },
                  { path: 'list-group', element: <ListGroups /> },
                  { path: 'modals', element: <Modals /> },
                  { path: 'offcanvas', element: <Offcanvas /> },
                  {
                    path: 'navs-and-tabs',
                    children: [
                      { path: 'navs', element: <Navs /> },
                      { path: 'navbar', element: <Navbars /> },
                      { path: 'vertical-navbar', element: <VerticalNavbar /> },
                      { path: 'top-navbar', element: <NavBarTop /> },
                      { path: 'double-top-navbar', element: <NavbarDoubleTop /> },
                      { path: 'combo-navbar', element: <ComboNavbar /> },
                      { path: 'tabs', element: <Tabs /> }
                    ]
                  },
                  {
                    path: 'pictures',
                    children: [
                      { path: 'avatar', element: <Avatar /> },
                      { path: 'images', element: <Image /> },
                      { path: 'figures', element: <Figures /> },
                      { path: 'hoverbox', element: <Hoverbox /> },
                      { path: 'lightbox', element: <Lightbox /> }
                    ]
                  },
                  { path: 'progress-bar', element: <BasicProgressBar /> },
                  { path: 'pagination', element: <Pagination /> },
                  { path: 'placeholder', element: <Placeholder /> },
                  { path: 'popovers', element: <Popovers /> },
                  { path: 'scrollspy', element: <Scrollspy /> },
                  { path: 'search', element: <Search /> },
                  { path: 'spinners', element: <Spinners /> },
                  { path: 'timeline', element: <Timeline /> },
                  { path: 'toasts', element: <Toasts /> },
                  { path: 'tooltips', element: <Tooltips /> },
                  { path: 'treeview', element: <TreeviewExample /> },
                  { path: 'typed-text', element: <TypedText /> },
                  {
                    path: 'videos',
                    children: [
                      { path: 'embed', element: <Embed /> },
                      { path: 'react-player', element: <ReactPlayerExample /> }
                    ]
                  }
                ]
              },
              // Reference Forms
              {
                path: 'forms',
                children: [
                  {
                    path: 'basic',
                    children: [
                      { path: 'form-control', element: <FormControl /> },
                      { path: 'input-group', element: <InputGroup /> },
                      { path: 'select', element: <Select /> },
                      { path: 'checks', element: <Checks /> },
                      { path: 'range', element: <Range /> },
                      { path: 'layout', element: <FormLayout /> }
                    ]
                  },
                  {
                    path: 'advance',
                    children: [
                      { path: 'advance-select', element: <AdvanceSelect /> },
                      { path: 'date-picker', element: <DatePicker /> },
                      { path: 'editor', element: <Editor /> },
                      { path: 'emoji-button', element: <EmojiPickerExample /> },
                      { path: 'file-uploader', element: <FileUploader /> },
                      { path: 'input-mask', element: <InputMaskExample /> },
                      { path: 'range-slider', element: <RangeSlider /> },
                      { path: 'rating', element: <Rating /> }
                    ]
                  },
                  { path: 'floating-labels', element: <FloatingLabels /> },
                  { path: 'wizard', element: <WizardForms /> },
                  { path: 'validation', element: <FormValidation /> }
                ]
              },
              // Reference Tables
              {
                path: 'tables',
                element: <Suspense key="ref-tables" fallback={<FalconLoader />}><Tables /></Suspense>
              },
              // Reference Icons
              {
                path: 'icons',
                children: [
                  { path: 'font-awesome', element: <FontAwesome /> },
                  { path: 'react-icons', element: <ReactIcons /> }
                ]
              },
              // Reference Maps
              {
                path: 'maps',
                children: [
                  { path: 'google', element: <Suspense key="ref-maps-google" fallback={<FalconLoader />}><GoogleMapExample /></Suspense> },
                  { path: 'leaflet', element: <LeafletMapExample /> }
                ]
              },
              // Reference Widgets
              { path: 'widgets', element: <Suspense key="ref-widgets" fallback={<FalconLoader />}><Widgets /></Suspense> },
              // Reference Charts
              {
                path: 'charts',
                children: [
                  { path: 'chartjs', element: <Suspense key="ref-charts-chartjs" fallback={<FalconLoader />}><Chartjs /></Suspense> },
                  { path: 'd3js', element: <Suspense key="ref-charts-d3js" fallback={<FalconLoader />}><D3js /></Suspense> },
                  {
                    path: 'echarts',
                    children: [
                      { path: 'how-to-use', element: <HowToUse /> },
                      { path: 'line-charts', element: <Suspense key="ref-echarts-line" fallback={<FalconLoader />}><LineCharts /></Suspense> },
                      { path: 'bar-charts', element: <Suspense key="ref-echarts-bar" fallback={<FalconLoader />}><BarCharts /></Suspense> },
                      { path: 'candlestick-charts', element: <Suspense key="ref-echarts-candlestick" fallback={<FalconLoader />}><CandlestickCharts /></Suspense> },
                      { path: 'geo-map', element: <Suspense key="ref-echarts-geo" fallback={<FalconLoader />}><GeoMaps /></Suspense> },
                      { path: 'scatter-charts', element: <Suspense key="ref-echarts-scatter" fallback={<FalconLoader />}><ScatterCharts /></Suspense> },
                      { path: 'pie-charts', element: <Suspense key="ref-echarts-pie" fallback={<FalconLoader />}><PieCharts /></Suspense> },
                      { path: 'radar-charts', element: <Suspense key="ref-echarts-radar" fallback={<FalconLoader />}><RadarCharts /></Suspense> },
                      { path: 'heatmap-charts', element: <Suspense key="ref-echarts-heatmap" fallback={<FalconLoader />}><HeatmapCharts /></Suspense> }
                    ]
                  }
                ]
              },
              // Reference Utilities
              {
                path: 'utilities',
                children: [
                  { path: 'background', element: <Background /> },
                  { path: 'borders', element: <Borders /> },
                  { path: 'colors', element: <Colors /> },
                  { path: 'colored-links', element: <ColoredLinks /> },
                  { path: 'display', element: <Display /> },
                  { path: 'visibility', element: <Visibility /> },
                  { path: 'stretched-link', element: <StretchedLink /> },
                  { path: 'float', element: <Float /> },
                  { path: 'position', element: <Position /> },
                  { path: 'spacing', element: <Spacing /> },
                  { path: 'sizing', element: <Sizing /> },
                  { path: 'text-truncation', element: <TextTruncation /> },
                  { path: 'typography', element: <Typography /> },
                  { path: 'vertical-align', element: <VerticalAlign /> },
                  { path: 'flex', element: <Flex /> },
                  { path: 'grid', element: <Grid /> },
                  { path: 'scroll-bar', element: <Scrollbar /> }
                ]
              },
              // Reference Pages
              {
                path: 'pages',
                children: [
                  { path: 'landing', element: <Landing /> },
                  { path: 'starter', element: <Starter /> },
                  {
                    path: 'pricing',
                    children: [
                      { path: 'default', element: <PricingDefault /> },
                      { path: 'alt', element: <PricingAlt /> }
                    ]
                  },
                  {
                    path: 'faq',
                    children: [
                      { path: 'basic', element: <FaqBasic /> },
                      { path: 'alt', element: <FaqAlt /> },
                      { path: 'accordion', element: <FaqAccordion /> }
                    ]
                  },
                  {
                    path: 'miscellaneous',
                    children: [
                      { path: 'associations', element: <Associations /> },
                      { path: 'invite-people', element: <InvitePeople /> },
                      { path: 'privacy-policy', element: <PrivacyPolicy /> }
                    ]
                  },
                  {
                    path: 'layouts',
                    children: [
                      { path: 'vertical-nav', element: <Suspense key="ref-layout-vertical" fallback={<FalconLoader />}><Dashboard /></Suspense> },
                      { path: 'top-nav', element: <Suspense key="ref-layout-top" fallback={<FalconLoader />}><Dashboard /></Suspense> },
                      { path: 'double-top', element: <Suspense key="ref-layout-doubletop" fallback={<FalconLoader />}><Dashboard /></Suspense> },
                      { path: 'combo-nav', element: <Suspense key="ref-layout-combo" fallback={<FalconLoader />}><Dashboard /></Suspense> }
                    ]
                  }
                ]
              },
              // Reference Documentation
              {
                path: 'documentation',
                children: [
                  { path: 'getting-started', element: <GettingStarted /> },
                  { path: 'configuration', element: <Configuration /> },
                  { path: 'styling', element: <Styling /> },
                  { path: 'dark-mode', element: <DarkMode /> },
                  { path: 'plugins', element: <Plugins /> },
                  { path: 'faq', element: <Faq /> },
                  { path: 'design-file', element: <DesignFile /> },
                  { path: 'changelog', element: <Changelog /> },
                  { path: 'migration', element: <Migration /> }
                ]
              },
              // Reference Test
              {
                path: 'test',
                children: [
                  { path: 'auth-test', element: <AuthTestPage /> },
                  { path: 'role-navigation', element: <RoleNavigationTester /> }
                ]
              }
            ]
          }
        ]
      },
      {
        path: '/test',
        element: (
          <ProtectedRoute>
            <MainLayout />
          </ProtectedRoute>
        ),
        children: [
          {
            path: 'role-navigation',
            element: <RoleNavigationTester />
          }
        ]
      },
      {
        path: '/',
        element: (
          <ProtectedRoute>
            <VerticalNavLayout />
          </ProtectedRoute>
        ),
        children: [
          {
            path: paths.verticalNavLayout,
            element: <Dashboard />
          }
        ]
      },
      {
        path: '/',
        element: (
          <ProtectedRoute>
            <TopNavLayout />
          </ProtectedRoute>
        ),
        children: [
          {
            path: paths.topNavLayout,
            element: <Dashboard />
          }
        ]
      },
      {
        path: '/',
        element: (
          <ProtectedRoute>
            <ComboNavLayout />
          </ProtectedRoute>
        ),
        children: [
          {
            path: paths.comboNavLayout,
            element: <Dashboard />
          }
        ]
      },
      {
        path: '/',
        element: (
          <ProtectedRoute>
            <DoubleTopNavLayout />
          </ProtectedRoute>
        ),
        children: [
          {
            path: paths.doubleTopNavLayout,
            element: <Dashboard />
          }
        ]
      },
      {
        path: '*',
        element: <Navigate to={paths.error404} replace />
      }
    ]
  }
];

export const router = createBrowserRouter(routes, {
  basename: import.meta.env.VITE_PUBLIC_URL
});

export default routes;
