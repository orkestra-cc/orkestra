// Dashboard Components - TypeScript Barrel Exports
// These are reusable dashboard components that can be used in any dashboard
// All components have been converted to TypeScript with proper type definitions

// Default Dashboard Components
export { default as WeeklySales } from './default/WeeklySales';
export { default as TotalOrder } from './default/TotalOrder';
export { default as MarketShare } from './default/MarketShare';
export { default as TotalSales } from './default/TotalSales';
export { default as RunningProjects } from './default/RunningProjects';
export { default as StorageStatus } from './default/StorageStatus';
export { default as SpaceWarning } from './default/SpaceWarning';
export { default as BestSellingProducts } from './default/BestSellingProducts';
export { default as SharedFiles } from './default/SharedFiles';
export { default as ActiveUsers } from './default/ActiveUsers';
export { default as BandwidthSaved } from './default/BandwidthSaved';
export { default as TopProducts } from './default/TopProducts';
export { default as Weather } from './default/Weather';

// Analytics Dashboard Components  
export { default as RealTimeUsers } from './analytics/real-time-users/RealTimeUsers';
export { default as Audience } from './analytics/audience/Audience';
export { default as ConnectCard } from './analytics/ConnectCard';
export { default as SessionByBrowser } from './analytics/session-by-browser/SessionByBrowser';
export { default as UsersByCountry } from './analytics/users-by-country/UsersByCountry';
export { default as Intelligence } from './analytics/Intelligence';
export { default as AnalyticsActiveUsers } from './analytics/active-users/ActiveUsers';
export { default as CampaignPerformance } from './analytics/campaign-perfomance/CampaignPerfomance';
export { default as UsersAtTime } from './analytics/users-at-a-time/UsersAtTime';
export { default as BounceRate } from './analytics/bounce-rate/BounceRate';
export { default as TrafficSource } from './analytics/traffic-source/TrafficSource';
export { default as AnalyticsStats } from './analytics/stats/Stats';
export { default as TopPages } from './analytics/top-pages/TopPages';

// CRM Dashboard Components
export { default as CrmStats } from './crm/CrmStats';
export { default as DealForecastBar } from './crm/DealForecastBar';
export { default as LocationBySession } from './crm/LocationBySession/LocationBySession';
export { default as CrmStatsChart } from './crm/StatsChart';
export { default as CrmToDoList } from './crm/ToDoList';

// Project Management Dashboard Components
export { default as Discussion } from './project-management/Discussion';
export { default as ProjectGreetings } from './project-management/Greetings';
export { default as MemberInfo } from './project-management/MemberInfo';
export { default as MembersActivity } from './project-management/MembersActivity';
export { default as RecentActivity } from './project-management/RecentActivity';
export { default as ProjectRunningProjects } from './project-management/RunningProjects';
export { default as TeamProgress } from './project-management/TeamProgress';
export { default as ProjectToDoList } from './project-management/ToDoList';

// SaaS Dashboard Components
export { default as DepositeStatus } from './saas/DepositeStatus';
export { default as DoMoreCard } from './saas/DoMoreCard';
export { default as SaasActiveUser } from './saas/SaasActiveUser';
export { default as SaasConversion } from './saas/SaasConversion';
export { default as SaasRevenue } from './saas/SaasRevenue';
export { default as TransactionSummary } from './saas/TransactionSummary';

// Support Desk Dashboard Components
export { default as SupportGreetings } from './support-desk/Greetings';
export { default as TicketStatus } from './support-desk/TicketStatus';
export { default as SupportToDoList } from './support-desk/ToDoList';