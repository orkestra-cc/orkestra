// Common TypeScript interfaces for Dashboard Components
import { Card } from 'react-bootstrap';

// Common chart data types
export interface ChartData {
  [key: string]: number[];
}

export interface TimeSeriesData {
  dates: string[];
  values: number[];
}

// Common theme colors used across components
export type ThemeColor = 
  | 'primary' 
  | 'secondary' 
  | 'success' 
  | 'info' 
  | 'warning' 
  | 'danger' 
  | 'light' 
  | 'dark';

// ECharts specific types
export interface EChartsOptions {
  [key: string]: any;
}

export type EChartsSeriesType = 'line' | 'bar' | 'pie' | 'scatter' | 'candlestick';

// Analytics data interfaces
export interface AnalyticsData {
  mobile: number[];
  desktop: number[];
  tablet: number[];
}

export interface TrafficSourceData {
  source: string;
  users: number;
  color: ThemeColor;
}

export interface CountryData {
  country: string;
  users: number;
  percentage: number;
}

export interface CampaignData {
  name: string;
  clicks: number;
  impressions: number;
  ctr: number;
  cost: number;
}

// CRM data interfaces
export interface CrmStatData {
  id: number;
  title: string;
  amount: number;
  target: string;
  icon: string;
  caret: string;
  color: ThemeColor;
  caretColor: ThemeColor;
  data: number[];
}

export interface LeadData {
  name: string;
  value: number;
  color: ThemeColor;
}

export interface RevenueData {
  period: string;
  amount: number;
  change: number;
}

// SaaS data interfaces
export interface SaasMetric {
  label: string;
  value: number;
  change: number;
  trend: 'up' | 'down' | 'stable';
}

export interface SubscriptionData {
  plan: string;
  subscribers: number;
  revenue: number;
  growth: number;
}

// Project Management interfaces
export interface ProjectData {
  id: string;
  name: string;
  progress: number;
  team: string[];
  deadline: string;
  status: 'active' | 'completed' | 'pending' | 'delayed';
}

export interface TeamMember {
  id: string;
  name: string;
  avatar: string;
  role: string;
  status: 'online' | 'offline' | 'away';
}

export interface TaskData {
  id: string;
  title: string;
  description?: string;
  assignee: string;
  priority: 'high' | 'medium' | 'low';
  status: 'todo' | 'in-progress' | 'done';
  dueDate: string;
}

// Support Desk interfaces
export interface TicketData {
  id: string;
  subject: string;
  customer: string;
  priority: 'high' | 'medium' | 'low';
  status: 'open' | 'in-progress' | 'resolved' | 'closed';
  assignee: string;
  createdAt: string;
  updatedAt: string;
}

export interface CustomerSatisfactionData {
  period: string;
  satisfaction: number;
  responses: number;
}

// Market Share data
export interface MarketShareData {
  name: string;
  value: number;
  color: string;
}

// Common widget prop interfaces
export interface BaseWidgetProps {
  className?: string;
  style?: React.CSSProperties;
}

export interface ChartWidgetProps extends BaseWidgetProps {
  data: number[] | ChartData;
  height?: number | string;
  width?: number | string;
}

// Theme context type
export interface ThemeContextType {
  getThemeColor: (color: string) => string;
  isDark: boolean;
  isRTL: boolean;
}

// Generic dropdown option interface
export interface DropdownOption {
  label: string;
  value: string | number;
}

// Common props for cards with dropdowns
export interface CardWithDropdownProps extends React.ComponentProps<typeof Card> {
  title?: string;
  showDropdown?: boolean;
  dropdownOptions?: DropdownOption[];
}