/**
 * React Orkestra Hooks
 *
 * Redux + RTK Query hooks for data fetching and state management.
 * All server state managed through RTK Query, client state through Redux.
 */

// RTK Query API hooks (Primary data fetching)
export * from '../store/api';

// Enhanced RTK Query hooks
export * from './auth/useAuthRTK';
export * from './dashboard/useDashboardRTK';

// Feature-specific hook collections
export * from './auth';
export * from './analytics';
export * from './communication';
export * from './management';

// Redux hooks - individual exports (no index file)
// Note: useAuth and useCurrentUser from ./redux/useAuth may conflict with auth hooks
// Import them explicitly if needed: import { useAuth } from 'hooks/redux/useAuth'
export { useKanban, useKanbanItems, useKanbanModal, useKanbanMembers, useKanbanLabels, useKanbanAttachments, useKanbanComments, useKanbanActivities, useCardHeight, useCurrentUser as useKanbanCurrentUser, useKanbanItemById, useTaskById, useTasksByColumnId } from './redux/useKanban';

// Data Utilities & Common Patterns
export * from './data';

// UI Hooks & Utilities
export * from './ui';