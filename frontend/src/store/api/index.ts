// RTK Query API slices and hooks
export { baseApi, invalidateApiTags } from './baseApi';

// Authentication
export * from './authApi';
export type {
  LoginCredentials,
  LoginResponse,
  LogoutResponse
} from './authApi';

// Dashboard and Analytics
export * from './dashboardApi';
export type {
  DashboardStats,
  WeatherData,
  SalesData,
  OrdersData,
  ActiveUsersData,
  WeeklySalesData,
  BestSellingProduct,
  MarketShareData,
  RunningProject,
  StorageStatus,
  DashboardType,
  TimeRange
} from './dashboardApi';

// Communications (Chat and Email)
export * from './communicationsApi';
export type {
  ChatMessage,
  ChatChannel,
  ChatParticipant,
  Email,
  EmailAddress,
  EmailAttachment,
  EmailFolder,
  SendMessageRequest,
  SendEmailRequest,
  ChatMessagesResponse,
  EmailsResponse
} from './communicationsApi';

// Management (Events, Kanban, Support)
export * from './managementApi';
export type {
  Event,
  EventAttendee,
  KanbanBoard,
  KanbanColumn,
  KanbanTask,
  TaskAssignee,
  TaskTag,
  TaskAttachment,
  TaskComment,
  BoardMember,
  SupportTicket,
  TicketAssignee,
  TicketCustomer,
  TicketMessage,
  TicketAttachment,
  CreateEventRequest,
  MoveTaskRequest,
  CreateTicketRequest
} from './managementApi';