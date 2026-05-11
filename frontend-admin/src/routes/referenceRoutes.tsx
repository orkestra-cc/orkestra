/**
 * Reference routes — Falcon template examples and component showcases.
 * Only registered in development builds (gated by import.meta.env.DEV).
 */
import { Suspense, lazy } from 'react';
import type { RouteObject } from 'react-router';
import { rootPaths } from './paths';
import paths from './paths';
import FalconLoader from 'components/common/FalconLoader';
import ProtectedRoute from 'components/authentication/ProtectedRoute';

// Layouts
import MainLayout from 'layouts/MainLayout';
import VerticalNavLayout from 'layouts/VerticalNavLayout';
import TopNavLayout from 'layouts/TopNavLayout';
import ComboNavLayout from 'layouts/ComboNavLayout';
import DoubleTopNavLayout from 'layouts/DoubleTopNavLayout';

// --- Lazy reference components ---
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
const BootstrapCarousel = lazy(() => import('reference/components/media/BootstrapCarousel'));
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
const Placeholder = lazy(() => import('reference/components/ui/Placeholder'));
const Lightbox = lazy(() => import('reference/components/media/Lightbox'));
const Calendar = lazy(() => import('reference/app-examples/calendar/Calendar'));
const Kanban = lazy(() => import('reference/app-examples/kanban/Kanban'));
const LineCharts = lazy(() => import('reference/charts/echarts/line-charts'));
const BarCharts = lazy(() => import('reference/charts/echarts/bar-charts'));
const CandlestickCharts = lazy(() => import('reference/charts/echarts/candlestick-charts'));
const GeoMaps = lazy(() => import('reference/charts/echarts/geo-map'));
const ScatterCharts = lazy(() => import('reference/charts/echarts/scatter-charts'));
const PieCharts = lazy(() => import('reference/charts/echarts/pie-charts'));
const RadarCharts = lazy(() => import('reference/charts/echarts/radar-charts/Index'));
const HeatmapCharts = lazy(() => import('reference/charts/echarts/heatmap-chart'));
const GoogleMapExample = lazy(() => import('reference/components/maps/GoogleMapExample'));
const Widgets = lazy(() => import('reference/components/widgets/Widgets'));
const ProjectManagement = lazy(() => import('reference/dashboards/ProjectManagementDashboard'));
const Migration = lazy(() => import('reference/documentation/migration/Migration'));
const Dashboard = lazy(() => import('reference/dashboards/DefaultDashboard'));
const SupportDesk = lazy(() => import('reference/dashboards/SupportDeskDashboard'));

// Direct imports (not lazy — small components)
import Landing from 'reference/pages/landing/Landing';
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
import DraggableExample from 'reference/components/misc/DraggableExample';
import HowToUse from 'reference/charts/echarts/HowToUse';
import LeafletMapExample from 'reference/components/maps/LeafletMapExample';
import CookieNoticeExample from 'reference/components/misc/CookieNoticeExample';
import Scrollbar from 'reference/components/misc/Scrollbar';
import Scrollspy from 'reference/components/misc/Scrollspy';
import ReactIcons from 'reference/components/icons/ReactIcons';
import ReactPlayerExample from 'reference/components/media/ReactPlayerExample';
import EmojiPickerExample from 'reference/components/forms/EmojiPicker';
import TreeviewExample from 'reference/components/ui/TreeviewExample';
import Timeline from 'reference/components/ui/Timeline';
import Faq from 'reference/documentation/Faq';
import InputMaskExample from 'reference/components/forms/InputMaskExample';
import RangeSlider from 'reference/components/forms/RangeSlider';
import Associations from 'reference/pages/associations/Associations';
import Followers from 'reference/app-examples/social/followers/Followers';
import Notifications from 'reference/app-examples/social/notifications/Notifications';
import ActivityLog from 'reference/app-examples/social/activity-log/ActivityLog';
import Feed from 'reference/app-examples/social/feed/Feed';
import TableView from 'reference/app-examples/support-desk/tickets-layout/TableView';
import CardView from 'reference/app-examples/support-desk/tickets-layout/CardView';
import Contacts from 'reference/app-examples/support-desk/contacts/Contacts';
import ContactDetails from 'reference/app-examples/support-desk/contact-details/ContactDetails';
import TicketsPreview from 'reference/app-examples/support-desk/tickets-preview/TicketsPreview';
import QuickLinks from 'reference/app-examples/support-desk/quick-links/QuickLinks';
import Reports from 'reference/app-examples/support-desk/reports/Reports';

/**
 * Returns reference route children for the MainLayout.
 * In production builds, returns an empty array — no reference routes are registered.
 */
export function getReferenceRoutes(): RouteObject[] {
  if (import.meta.env.PROD && !import.meta.env.VITE_ENABLE_REFERENCE) {
    return [];
  }

  return [
    // Legacy Falcon routes (top-level paths used by the original template)
    {
      path: rootPaths.dashboardRoot,
      children: [
        {
          path: paths.analytics,
          element: <Suspense key="dashboard-analytics" fallback={<FalconLoader />}><Analytics /></Suspense>,
        },
        {
          path: paths.crm,
          element: <Suspense key="dashboard-crm" fallback={<FalconLoader />}><Crm /></Suspense>,
        },
        {
          path: paths.saas,
          element: <Suspense key="dashboard-sass" fallback={<FalconLoader />}><Saas /></Suspense>,
        },
        {
          path: paths.projectManagement,
          element: <Suspense key="dashboard-projectManagement" fallback={<FalconLoader />}><ProjectManagement /></Suspense>,
        },
        {
          path: paths.supportDesk,
          element: <Suspense key="dashboard-supportDesk" fallback={<FalconLoader />}><SupportDesk /></Suspense>,
        },
      ],
    },
    {
      path: rootPaths.appsRoot,
      children: [
        { path: paths.calendar, element: <Suspense key="calendar" fallback={<FalconLoader />}><Calendar /></Suspense> },
        { path: paths.chat, element: <Chat /> },
        { path: paths.kanban, element: <Suspense key="kanban" fallback={<FalconLoader />}><Kanban /></Suspense> },
      ],
    },
    {
      path: rootPaths.emailRoot,
      children: [
        { path: paths.emailInbox, element: <Inbox /> },
        { path: paths.emailDetail, element: <EmailDetail /> },
        { path: paths.emailCompose, element: <Compose /> },
      ],
    },
    {
      path: rootPaths.eventsRoot,
      children: [
        { path: paths.createEvent, element: <CreateEvent /> },
        { path: paths.eventDetail, element: <EventDetail /> },
        { path: paths.eventList, element: <EventList /> },
      ],
    },
    {
      path: rootPaths.socialRoot,
      children: [
        { path: paths.feed, element: <Feed /> },
        { path: paths.activityLog, element: <ActivityLog /> },
        { path: paths.notifications, element: <Notifications /> },
        { path: paths.followers, element: <Followers /> },
      ],
    },
    {
      path: rootPaths.supportDeskRoot,
      children: [
        { path: paths.ticketsTable, element: <TableView /> },
        { path: paths.ticketsCard, element: <CardView /> },
        { path: paths.contacts, element: <Contacts /> },
        { path: paths.contactDetails, element: <ContactDetails /> },
        { path: paths.ticketsPreview, element: <TicketsPreview /> },
        { path: paths.quickLinks, element: <QuickLinks /> },
        { path: paths.reports, element: <Reports /> },
      ],
    },
    {
      path: rootPaths.pagesRoot,
      children: [{ path: paths.starter, element: <Starter /> }],
    },
    {
      path: rootPaths.pricingRoot,
      children: [
        { path: paths.pricingDefault, element: <PricingDefault /> },
        { path: paths.pricingAlt, element: <PricingAlt /> },
      ],
    },
    {
      path: rootPaths.faqRoot,
      children: [
        { path: paths.faqBasic, element: <FaqBasic /> },
        { path: paths.faqAlt, element: <FaqAlt /> },
        { path: paths.faqAccordion, element: <FaqAccordion /> },
      ],
    },
    {
      path: rootPaths.miscRoot,
      children: [
        { path: paths.associations, element: <Associations /> },
        { path: paths.invitePeople, element: <InvitePeople /> },
        { path: paths.privacyPolicy, element: <PrivacyPolicy /> },
        { path: paths.authTest, element: <AuthTestPage /> },
      ],
    },
    // Forms
    {
      path: rootPaths.formsRoot,
      children: [
        {
          path: rootPaths.basicFormsRoot,
          children: [
            { path: paths.formControl, element: <FormControl /> },
            { path: paths.inputGroup, element: <InputGroup /> },
            { path: paths.select, element: <Select /> },
            { path: paths.checks, element: <Checks /> },
            { path: paths.range, element: <Range /> },
            { path: paths.formLayout, element: <FormLayout /> },
          ],
        },
        {
          path: rootPaths.advanceFormsRoot,
          children: [
            { path: paths.advanceSelect, element: <AdvanceSelect /> },
            { path: paths.datePicker, element: <DatePicker /> },
            { path: paths.editor, element: <Editor /> },
            { path: paths.emojiButton, element: <EmojiPickerExample /> },
            { path: paths.fileUploader, element: <FileUploader /> },
            { path: paths.inputMask, element: <InputMaskExample /> },
            { path: paths.rangeSlider, element: <RangeSlider /> },
            { path: paths.rating, element: <Rating /> },
          ],
        },
        { path: paths.floatingLabels, element: <FloatingLabels /> },
        { path: paths.wizard, element: <WizardForms /> },
        { path: paths.validation, element: <FormValidation /> },
      ],
    },
    {
      path: rootPaths.tableRoot,
      element: <Suspense key="tables" fallback={<FalconLoader />}><Tables /></Suspense>,
    },
    // Charts (echarts only — chartjs and d3js were removed; production code uses echarts)
    {
      path: rootPaths.chartsRoot,
      children: [
        {
          path: rootPaths.echartsRoot,
          children: [
            { path: paths.echartsHowToUse, element: <HowToUse /> },
            { path: paths.lineCharts, element: <Suspense key="echarts-lineChart" fallback={<FalconLoader />}><LineCharts /></Suspense> },
            { path: paths.barCharts, element: <Suspense key="echarts-barChart" fallback={<FalconLoader />}><BarCharts /></Suspense> },
            { path: paths.candlestickCharts, element: <Suspense key="echarts-candleStick" fallback={<FalconLoader />}><CandlestickCharts /></Suspense> },
            { path: paths.geoMap, element: <Suspense key="echarts-geoMap" fallback={<FalconLoader />}><GeoMaps /></Suspense> },
            { path: paths.scatterCharts, element: <Suspense key="echarts-scatterChart" fallback={<FalconLoader />}><ScatterCharts /></Suspense> },
            { path: paths.pieCharts, element: <Suspense key="echarts-pieChart" fallback={<FalconLoader />}><PieCharts /></Suspense> },
            { path: paths.radarCharts, element: <Suspense key="echarts-radarChart" fallback={<FalconLoader />}><RadarCharts /></Suspense> },
            { path: paths.heatmapCharts, element: <Suspense key="echarts-heatmapChart" fallback={<FalconLoader />}><HeatmapCharts /></Suspense> },
          ],
        },
      ],
    },
    // Icons
    {
      path: rootPaths.iconsRoot,
      children: [
        { path: paths.fontAwesome, element: <FontAwesome /> },
        { path: paths.reactIcons, element: <ReactIcons /> },
      ],
    },
    // Maps
    {
      path: rootPaths.mapsRoot,
      children: [
        { path: paths.googleMap, element: <Suspense key="googleMap" fallback={<FalconLoader />}><GoogleMapExample /></Suspense> },
        { path: paths.leafletMap, element: <Suspense key="leafletMap" fallback={<FalconLoader />}><LeafletMapExample /></Suspense> },
      ],
    },
    // Components showcase
    {
      path: rootPaths.componentsRoot,
      children: [
        { path: paths.alerts, element: <Suspense key="alerts" fallback={<FalconLoader />}><Alerts /></Suspense> },
        { path: paths.accordion, element: <Suspense key="accordion" fallback={<FalconLoader />}><Accordion /></Suspense> },
        { path: paths.animatedIcons, element: <AnimatedIcons /> },
        { path: paths.background, element: <Backgrounds /> },
        { path: paths.badges, element: <Suspense key="badges" fallback={<FalconLoader />}><Badges /></Suspense> },
        { path: paths.breadcrumbs, element: <Suspense key="breadcrumbs" fallback={<FalconLoader />}><Breadcrumbs /></Suspense> },
        { path: paths.buttons, element: <Suspense key="buttons" fallback={<FalconLoader />}><Buttons /></Suspense> },
        { path: paths.calendarExample, element: <Suspense key="calendarExample" fallback={<FalconLoader />}><CalendarExample /></Suspense> },
        { path: paths.cards, element: <Cards /> },
        {
          path: rootPaths.carouselRoot,
          children: [
            { path: paths.bootstrapCarousel, element: <BootstrapCarousel /> },
            { path: paths.slickCarousel, element: <SlickCarousel /> },
          ],
        },
        { path: paths.collapse, element: <Collapse /> },
        { path: paths.cookieNotice, element: <CookieNoticeExample /> },
        { path: paths.countup, element: <CountUp /> },
        { path: paths.draggable, element: <DraggableExample /> },
        { path: paths.dropdowns, element: <Dropdowns /> },
        { path: paths.listGroup, element: <ListGroups /> },
        { path: paths.modals, element: <Modals /> },
        { path: paths.offcanvas, element: <Offcanvas /> },
        {
          path: rootPaths.navsAndTabsRoot,
          children: [
            { path: paths.navs, element: <Navs /> },
            { path: paths.navbar, element: <Navbars /> },
            { path: paths.verticalNavbar, element: <VerticalNavbar /> },
            { path: paths.topNavbar, element: <NavBarTop /> },
            { path: paths.doubleTopNavbar, element: <NavbarDoubleTop /> },
            { path: paths.comboNavbar, element: <ComboNavbar /> },
            { path: paths.tabs, element: <Tabs /> },
          ],
        },
        {
          path: rootPaths.picturesRoot,
          children: [
            { path: paths.avatar, element: <Avatar /> },
            { path: paths.images, element: <Image /> },
            { path: paths.figures, element: <Figures /> },
            { path: paths.hoverbox, element: <Hoverbox /> },
            { path: paths.lightbox, element: <Lightbox /> },
          ],
        },
        { path: paths.progressBar, element: <BasicProgressBar /> },
        { path: paths.pagination, element: <Pagination /> },
        { path: paths.placeholder, element: <Placeholder /> },
        { path: paths.popovers, element: <Popovers /> },
        { path: paths.scrollspy, element: <Scrollspy /> },
        { path: paths.search, element: <Search /> },
        { path: paths.spinners, element: <Spinners /> },
        { path: paths.timeline, element: <Timeline /> },
        { path: paths.toasts, element: <Toasts /> },
        { path: paths.tooltips, element: <Tooltips /> },
        { path: paths.treeview, element: <TreeviewExample /> },
        { path: paths.typedText, element: <TypedText /> },
        {
          path: rootPaths.videosRoot,
          children: [
            { path: paths.embedVideo, element: <Embed /> },
            { path: paths.reactPlayer, element: <ReactPlayerExample /> },
          ],
        },
      ],
    },
    // Utilities
    {
      path: rootPaths.utilitiesRoot,
      children: [
        { path: paths.backgroundColor, element: <Background /> },
        { path: paths.borders, element: <Borders /> },
        { path: paths.colors, element: <Colors /> },
        { path: paths.coloredLinks, element: <ColoredLinks /> },
        { path: paths.display, element: <Display /> },
        { path: paths.flex, element: <Flex /> },
        { path: paths.float, element: <Float /> },
        { path: paths.grid, element: <Grid /> },
        { path: paths.scrollBar, element: <Scrollbar /> },
        { path: paths.position, element: <Position /> },
        { path: paths.spacing, element: <Spacing /> },
        { path: paths.sizing, element: <Sizing /> },
        { path: paths.stretchedLink, element: <StretchedLink /> },
        { path: paths.textTruncation, element: <TextTruncation /> },
        { path: paths.typography, element: <Typography /> },
        { path: paths.verticalAlign, element: <VerticalAlign /> },
        { path: paths.visibility, element: <Visibility /> },
      ],
    },
    // Documentation
    {
      path: rootPaths.docRoot,
      children: [
        { path: paths.gettingStarted, element: <GettingStarted /> },
        { path: paths.configuration, element: <Configuration /> },
        { path: paths.styling, element: <Styling /> },
        { path: paths.darkMode, element: <DarkMode /> },
        { path: paths.plugin, element: <Plugins /> },
        { path: paths.faq, element: <Faq /> },
        { path: paths.designFile, element: <DesignFile /> },
      ],
    },
    { path: paths.widgets, element: <Suspense key="widgets" fallback={<FalconLoader />}><Widgets /></Suspense> },
    { path: paths.changelog, element: <Changelog /> },
    { path: paths.migration, element: <Migration /> },
    // /reference/* organized tree
    {
      path: rootPaths.referenceRoot,
      children: [
        {
          path: 'dashboards',
          children: [
            { path: 'default', element: <Suspense key="ref-dashboard-default" fallback={<FalconLoader />}><Dashboard /></Suspense> },
            { path: 'analytics', element: <Suspense key="ref-dashboard-analytics" fallback={<FalconLoader />}><Analytics /></Suspense> },
            { path: 'crm', element: <Suspense key="ref-dashboard-crm" fallback={<FalconLoader />}><Crm /></Suspense> },
            { path: 'saas', element: <Suspense key="ref-dashboard-saas" fallback={<FalconLoader />}><Saas /></Suspense> },
            { path: 'project-management', element: <Suspense key="ref-dashboard-pm" fallback={<FalconLoader />}><ProjectManagement /></Suspense> },
            { path: 'support-desk', element: <Suspense key="ref-dashboard-supportdesk" fallback={<FalconLoader />}><SupportDesk /></Suspense> },
          ],
        },
        {
          path: 'app-examples',
          children: [
            { path: 'calendar', element: <Suspense key="ref-app-calendar" fallback={<FalconLoader />}><Calendar /></Suspense> },
            { path: 'chat', element: <Chat /> },
            { path: 'kanban', element: <Suspense key="ref-app-kanban" fallback={<FalconLoader />}><Kanban /></Suspense> },
            {
              path: 'email',
              children: [
                { path: 'inbox', element: <Inbox /> },
                { path: 'compose', element: <Compose /> },
                { path: 'detail', element: <EmailDetail /> },
              ],
            },
            {
              path: 'events',
              children: [
                { path: 'create', element: <CreateEvent /> },
                { path: 'list', element: <EventList /> },
                { path: 'detail', element: <EventDetail /> },
              ],
            },
            {
              path: 'social',
              children: [
                { path: 'feed', element: <Feed /> },
                { path: 'activity-log', element: <ActivityLog /> },
                { path: 'notifications', element: <Notifications /> },
                { path: 'followers', element: <Followers /> },
              ],
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
                { path: 'reports', element: <Reports /> },
              ],
            },
          ],
        },
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
                { path: 'slick', element: <SlickCarousel /> },
              ],
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
                { path: 'tabs', element: <Tabs /> },
              ],
            },
            {
              path: 'pictures',
              children: [
                { path: 'avatar', element: <Avatar /> },
                { path: 'images', element: <Image /> },
                { path: 'figures', element: <Figures /> },
                { path: 'hoverbox', element: <Hoverbox /> },
                { path: 'lightbox', element: <Lightbox /> },
              ],
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
                { path: 'react-player', element: <ReactPlayerExample /> },
              ],
            },
          ],
        },
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
                { path: 'layout', element: <FormLayout /> },
              ],
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
                { path: 'rating', element: <Rating /> },
              ],
            },
            { path: 'floating-labels', element: <FloatingLabels /> },
            { path: 'wizard', element: <WizardForms /> },
            { path: 'validation', element: <FormValidation /> },
          ],
        },
        { path: 'tables', element: <Suspense key="ref-tables" fallback={<FalconLoader />}><Tables /></Suspense> },
        {
          path: 'icons',
          children: [
            { path: 'font-awesome', element: <FontAwesome /> },
            { path: 'react-icons', element: <ReactIcons /> },
          ],
        },
        {
          path: 'maps',
          children: [
            { path: 'google', element: <Suspense key="ref-maps-google" fallback={<FalconLoader />}><GoogleMapExample /></Suspense> },
            { path: 'leaflet', element: <LeafletMapExample /> },
          ],
        },
        { path: 'widgets', element: <Suspense key="ref-widgets" fallback={<FalconLoader />}><Widgets /></Suspense> },
        {
          path: 'charts',
          children: [
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
                { path: 'heatmap-charts', element: <Suspense key="ref-echarts-heatmap" fallback={<FalconLoader />}><HeatmapCharts /></Suspense> },
              ],
            },
          ],
        },
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
            { path: 'scroll-bar', element: <Scrollbar /> },
          ],
        },
        {
          path: 'pages',
          children: [
            { path: 'landing', element: <Landing /> },
            { path: 'starter', element: <Starter /> },
            {
              path: 'pricing',
              children: [
                { path: 'default', element: <PricingDefault /> },
                { path: 'alt', element: <PricingAlt /> },
              ],
            },
            {
              path: 'faq',
              children: [
                { path: 'basic', element: <FaqBasic /> },
                { path: 'alt', element: <FaqAlt /> },
                { path: 'accordion', element: <FaqAccordion /> },
              ],
            },
            {
              path: 'miscellaneous',
              children: [
                { path: 'associations', element: <Associations /> },
                { path: 'invite-people', element: <InvitePeople /> },
                { path: 'privacy-policy', element: <PrivacyPolicy /> },
              ],
            },
            {
              path: 'layouts',
              children: [
                { path: 'vertical-nav', element: <Suspense key="ref-layout-vertical" fallback={<FalconLoader />}><Dashboard /></Suspense> },
                { path: 'top-nav', element: <Suspense key="ref-layout-top" fallback={<FalconLoader />}><Dashboard /></Suspense> },
                { path: 'double-top', element: <Suspense key="ref-layout-doubletop" fallback={<FalconLoader />}><Dashboard /></Suspense> },
                { path: 'combo-nav', element: <Suspense key="ref-layout-combo" fallback={<FalconLoader />}><Dashboard /></Suspense> },
              ],
            },
          ],
        },
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
            { path: 'migration', element: <Migration /> },
          ],
        },
        {
          path: 'test',
          children: [
            { path: 'auth-test', element: <AuthTestPage /> },
            { path: 'role-navigation', element: <RoleNavigationTester /> },
          ],
        },
      ],
    },
  ];
}

/**
 * Returns top-level layout variant routes (outside the MainLayout).
 * These are reference routes for testing different nav layouts.
 */
export function getLayoutVariantRoutes(): RouteObject[] {
  if (import.meta.env.PROD && !import.meta.env.VITE_ENABLE_REFERENCE) {
    return [];
  }

  return [
    {
      path: '/test',
      element: (
        <ProtectedRoute>
          <MainLayout />
        </ProtectedRoute>
      ),
      children: [
        { path: 'role-navigation', element: <RoleNavigationTester /> },
      ],
    },
    {
      path: '/',
      element: <ProtectedRoute><VerticalNavLayout /></ProtectedRoute>,
      children: [{ path: paths.verticalNavLayout, element: <Dashboard /> }],
    },
    {
      path: '/',
      element: <ProtectedRoute><TopNavLayout /></ProtectedRoute>,
      children: [{ path: paths.topNavLayout, element: <Dashboard /> }],
    },
    {
      path: '/',
      element: <ProtectedRoute><ComboNavLayout /></ProtectedRoute>,
      children: [{ path: paths.comboNavLayout, element: <Dashboard /> }],
    },
    {
      path: '/',
      element: <ProtectedRoute><DoubleTopNavLayout /></ProtectedRoute>,
      children: [{ path: paths.doubleTopNavLayout, element: <Dashboard /> }],
    },
  ];
}
