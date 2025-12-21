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
const Associations = lazy(() => import('reference/pages/asscociations/Associations'));
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
const AdvanceTableExamples = lazy(
  () => import('reference/components/tables/AdvanceTableExamples')
);
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
import SocialAuthCallback from 'components/authentication/SocialAuthCallback';
const Dashboard = lazy(() => import('reference/dashboards/DefaultDashboard'));
import Faq from 'reference/documentation/Faq';
const SupportDesk = lazy(() => import('reference/dashboards/SupportDeskDashboard'));
const UserManagement = lazy(
  () => import('reference/dashboards/UserManagementDashboard')
);
const AdminUserProfile = lazy(
  () => import('pages/admin/user-profile/AdminUserProfile')
);
const DeadlineReports = lazy(
  () => import('pages/admin/Reports/DeadlineReports')
);
const OperatorProfile = lazy(
  () => import('pages/operator/profile/OperatorProfile')
);
const VehicleManagement = lazy(
  () => import('reference/dashboards/VehicleManagementDashboard')
);
const VehicleProfile = lazy(
  () => import('pages/fleet/vehicle-profile/VehicleProfile')
);
const CraneManagement = lazy(
  () => import('reference/dashboards/CraneManagementDashboard')
);
const CraneProfile = lazy(
  () => import('pages/fleet/crane-profile/CraneProfile')
);
const TachographManagement = lazy(
  () => import('reference/dashboards/TachographManagementDashboard')
);
const TachographProfile = lazy(
  () => import('pages/fleet/tachograph-profile/TachographProfile')
);
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
                path: 'reports/deadlines',
                element: (
                  <ProtectedRoute
                    requiredPermissions={[
                      ['developer', 'ceo', 'administrator', 'manager']
                    ]}
                  >
                    <Suspense
                      key="admin-deadlineReports"
                      fallback={<FalconLoader />}
                    >
                      <DeadlineReports />
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
              }
            ]
          },
          {
            path: 'fleet/vehicles',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="fleet-vehicleManagement"
                  fallback={<FalconLoader />}
                >
                  <VehicleManagement />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'fleet/vehicle/:vehicleId',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="fleet-vehicleProfile"
                  fallback={<FalconLoader />}
                >
                  <VehicleProfile />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'fleet/cranes',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="fleet-craneManagement"
                  fallback={<FalconLoader />}
                >
                  <CraneManagement />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'fleet/crane/:craneId',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense key="fleet-craneProfile" fallback={<FalconLoader />}>
                  <CraneProfile />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'fleet/tachographs',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="fleet-tachographManagement"
                  fallback={<FalconLoader />}
                >
                  <TachographManagement />
                </Suspense>
              </ProtectedRoute>
            )
          },
          {
            path: 'fleet/tachograph/:tachographId',
            element: (
              <ProtectedRoute
                requiredPermissions={[
                  ['developer', 'ceo', 'administrator']
                ]}
              >
                <Suspense
                  key="fleet-tachographProfile"
                  fallback={<FalconLoader />}
                >
                  <TachographProfile />
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
            children: [
              {
                path: paths.basicTables,
                element: (
                  <Suspense key="tables" fallback={<FalconLoader />}>
                    <Tables />
                  </Suspense>
                )
              },
              {
                path: paths.advanceTables,
                element: (
                  <Suspense key="advanceTables" fallback={<FalconLoader />}>
                    <AdvanceTableExamples />
                  </Suspense>
                )
              }
            ]
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
