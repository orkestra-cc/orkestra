import { baseApi } from './baseApi';

// Events types
export interface Event {
  id: string;
  title: string;
  description: string;
  start: string;
  end: string;
  allDay: boolean;
  type: 'meeting' | 'deadline' | 'reminder' | 'task' | 'other';
  priority: 'low' | 'medium' | 'high';
  attendees: EventAttendee[];
  location?: string;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface EventAttendee {
  id: string;
  name: string;
  email: string;
  status: 'pending' | 'accepted' | 'declined' | 'tentative';
}

// Kanban types
export interface KanbanBoard {
  id: string;
  title: string;
  description?: string;
  columns: KanbanColumn[];
  members: BoardMember[];
  createdAt: string;
  updatedAt: string;
}

export interface KanbanColumn {
  id: string;
  title: string;
  position: number;
  tasks: KanbanTask[];
  limits?: {
    min?: number;
    max?: number;
  };
}

export interface KanbanTask {
  id: string;
  title: string;
  description?: string;
  priority: 'low' | 'medium' | 'high' | 'urgent';
  status: string;
  assignees: TaskAssignee[];
  tags: TaskTag[];
  dueDate?: string;
  createdAt: string;
  updatedAt: string;
  position: number;
  columnId: string;
  attachments?: TaskAttachment[];
  comments?: TaskComment[];
}

export interface TaskAssignee {
  id: string;
  name: string;
  avatar?: string;
  email: string;
}

export interface TaskTag {
  id: string;
  name: string;
  color: string;
}

export interface TaskAttachment {
  id: string;
  filename: string;
  size: number;
  url: string;
  uploadedAt: string;
}

export interface TaskComment {
  id: string;
  content: string;
  authorId: string;
  authorName: string;
  createdAt: string;
}

export interface BoardMember {
  id: string;
  name: string;
  email: string;
  avatar?: string;
  role: 'owner' | 'admin' | 'member';
}

// Support Desk types
export interface SupportTicket {
  id: string;
  title: string;
  description: string;
  status: 'open' | 'in-progress' | 'resolved' | 'closed' | 'pending';
  priority: 'low' | 'medium' | 'high' | 'urgent';
  category: string;
  assignedTo?: TicketAssignee;
  customer: TicketCustomer;
  messages: TicketMessage[];
  tags: string[];
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string;
  dueDate?: string;
}

export interface TicketAssignee {
  id: string;
  name: string;
  email: string;
  avatar?: string;
}

export interface TicketCustomer {
  id: string;
  name: string;
  email: string;
  company?: string;
}

export interface TicketMessage {
  id: string;
  content: string;
  authorId: string;
  authorName: string;
  isInternal: boolean;
  attachments?: TicketAttachment[];
  createdAt: string;
}

export interface TicketAttachment {
  id: string;
  filename: string;
  size: number;
  mimeType: string;
  url: string;
}

// Request types
export interface CreateEventRequest {
  title: string;
  description: string;
  start: string;
  end: string;
  allDay: boolean;
  type: Event['type'];
  priority: Event['priority'];
  attendees: string[];
  location?: string;
}

export interface MoveTaskRequest {
  taskId: string;
  fromColumnId: string;
  toColumnId: string;
  position: number;
}

export interface CreateTicketRequest {
  title: string;
  description: string;
  priority: SupportTicket['priority'];
  category: string;
  customerId: string;
}

// Management API slice
export const managementApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Events endpoints
    getEvents: builder.query<Event[], { start?: string; end?: string }>({
      query: ({ start, end }) => {
        const params = new URLSearchParams();
        if (start) params.append('start', start);
        if (end) params.append('end', end);
        return `/events?${params.toString()}`;
      },
      providesTags: ['Events'],
      keepUnusedDataFor: 300,
    }),

    getEvent: builder.query<Event, string>({
      query: (eventId) => `/events/${eventId}`,
      providesTags: (_result, _error, eventId) => [
        { type: 'Events', id: eventId }
      ],
    }),

    createEvent: builder.mutation<Event, CreateEventRequest>({
      query: (event) => ({
        url: '/events',
        method: 'POST',
        body: event,
      }),
      invalidatesTags: ['Events'],
    }),

    updateEvent: builder.mutation<Event, { eventId: string; updates: Partial<CreateEventRequest> }>({
      query: ({ eventId, updates }) => ({
        url: `/events/${eventId}`,
        method: 'PUT',
        body: updates,
      }),
      invalidatesTags: (_result, _error, { eventId }) => [
        'Events',
        { type: 'Events', id: eventId }
      ],
    }),

    deleteEvent: builder.mutation<void, string>({
      query: (eventId) => ({
        url: `/events/${eventId}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, eventId) => [
        'Events',
        { type: 'Events', id: eventId }
      ],
    }),

    // Kanban endpoints
    getKanbanBoards: builder.query<KanbanBoard[], void>({
      query: () => '/kanban/boards',
      providesTags: ['Kanban'],
      keepUnusedDataFor: 300,
    }),

    getKanbanBoard: builder.query<KanbanBoard, string>({
      query: (boardId) => `/kanban/boards/${boardId}`,
      providesTags: (_result, _error, boardId) => [
        { type: 'Kanban', id: boardId }
      ],
    }),

    createKanbanBoard: builder.mutation<KanbanBoard, { title: string; description?: string }>({
      query: (board) => ({
        url: '/kanban/boards',
        method: 'POST',
        body: board,
      }),
      invalidatesTags: ['Kanban'],
    }),

    updateKanbanBoard: builder.mutation<KanbanBoard, { boardId: string; updates: Partial<KanbanBoard> }>({
      query: ({ boardId, updates }) => ({
        url: `/kanban/boards/${boardId}`,
        method: 'PUT',
        body: updates,
      }),
      invalidatesTags: (_result, _error, { boardId }) => [
        'Kanban',
        { type: 'Kanban', id: boardId }
      ],
    }),

    createKanbanTask: builder.mutation<KanbanTask, Omit<KanbanTask, 'id' | 'createdAt' | 'updatedAt'>>({
      query: (task) => ({
        url: '/kanban/tasks',
        method: 'POST',
        body: task,
      }),
      invalidatesTags: ['Kanban'],
    }),

    updateKanbanTask: builder.mutation<KanbanTask, { taskId: string; updates: Partial<KanbanTask> }>({
      query: ({ taskId, updates }) => ({
        url: `/kanban/tasks/${taskId}`,
        method: 'PUT',
        body: updates,
      }),
      invalidatesTags: ['Kanban'],
    }),

    moveKanbanTask: builder.mutation<KanbanTask, MoveTaskRequest>({
      query: (moveRequest) => ({
        url: '/kanban/tasks/move',
        method: 'PUT',
        body: moveRequest,
      }),
      invalidatesTags: ['Kanban'],
      // Optimistic update for smooth drag and drop
      onQueryStarted: async (_moveRequest, { queryFulfilled }) => {
        // You could implement optimistic updates here for better UX
        try {
          await queryFulfilled;
        } catch {
          // Revert optimistic update on failure
        }
      },
    }),

    deleteKanbanTask: builder.mutation<void, string>({
      query: (taskId) => ({
        url: `/kanban/tasks/${taskId}`,
        method: 'DELETE',
      }),
      invalidatesTags: ['Kanban'],
    }),

    // Support Desk endpoints
    getSupportTickets: builder.query<{
      tickets: SupportTicket[];
      totalCount: number;
      hasMore: boolean;
    }, {
      status?: string;
      priority?: string;
      assignedTo?: string;
      cursor?: string;
      limit?: number;
    }>({
      query: ({ status, priority, assignedTo, cursor, limit = 25 }) => {
        const params = new URLSearchParams();
        if (status) params.append('status', status);
        if (priority) params.append('priority', priority);
        if (assignedTo) params.append('assignedTo', assignedTo);
        if (cursor) params.append('cursor', cursor);
        params.append('limit', limit.toString());

        return `/support/tickets?${params.toString()}`;
      },
      providesTags: ['SupportTicket'],
      serializeQueryArgs: ({ queryArgs }) => {
        const { cursor, ...rest } = queryArgs;
        return `tickets-${JSON.stringify(rest)}`;
      },
      merge: (currentCache, newItems, { arg }) => {
        if (arg.cursor) {
          return {
            ...newItems,
            tickets: [...currentCache.tickets, ...newItems.tickets]
          };
        }
        return newItems;
      },
    }),

    getSupportTicket: builder.query<SupportTicket, string>({
      query: (ticketId) => `/support/tickets/${ticketId}`,
      providesTags: (_result, _error, ticketId) => [
        { type: 'SupportTicket', id: ticketId }
      ],
    }),

    createSupportTicket: builder.mutation<SupportTicket, CreateTicketRequest>({
      query: (ticket) => ({
        url: '/support/tickets',
        method: 'POST',
        body: ticket,
      }),
      invalidatesTags: ['SupportTicket'],
    }),

    updateSupportTicket: builder.mutation<SupportTicket, { ticketId: string; updates: Partial<SupportTicket> }>({
      query: ({ ticketId, updates }) => ({
        url: `/support/tickets/${ticketId}`,
        method: 'PUT',
        body: updates,
      }),
      invalidatesTags: (_result, _error, { ticketId }) => [
        'SupportTicket',
        { type: 'SupportTicket', id: ticketId }
      ],
    }),

    addTicketMessage: builder.mutation<TicketMessage, {
      ticketId: string;
      content: string;
      isInternal: boolean;
      attachments?: File[];
    }>({
      query: ({ ticketId, content, isInternal, attachments }) => ({
        url: `/support/tickets/${ticketId}/messages`,
        method: 'POST',
        body: { content, isInternal, attachments },
      }),
      invalidatesTags: (_result, _error, { ticketId }) => [
        'SupportTicket',
        { type: 'SupportTicket', id: ticketId }
      ],
    }),

    assignSupportTicket: builder.mutation<SupportTicket, { ticketId: string; assigneeId: string }>({
      query: ({ ticketId, assigneeId }) => ({
        url: `/support/tickets/${ticketId}/assign`,
        method: 'PUT',
        body: { assigneeId },
      }),
      invalidatesTags: (_result, _error, { ticketId }) => [
        'SupportTicket',
        { type: 'SupportTicket', id: ticketId }
      ],
    }),

    closeSupportTicket: builder.mutation<SupportTicket, { ticketId: string; resolution: string }>({
      query: ({ ticketId, resolution }) => ({
        url: `/support/tickets/${ticketId}/close`,
        method: 'PUT',
        body: { resolution },
      }),
      invalidatesTags: (_result, _error, { ticketId }) => [
        'SupportTicket',
        { type: 'SupportTicket', id: ticketId }
      ],
    }),
  }),
});

// Export hooks
export const {
  // Events
  useGetEventsQuery,
  useGetEventQuery,
  useCreateEventMutation,
  useUpdateEventMutation,
  useDeleteEventMutation,
  // Kanban
  useGetKanbanBoardsQuery,
  useGetKanbanBoardQuery,
  useCreateKanbanBoardMutation,
  useUpdateKanbanBoardMutation,
  useCreateKanbanTaskMutation,
  useUpdateKanbanTaskMutation,
  useMoveKanbanTaskMutation,
  useDeleteKanbanTaskMutation,
  // Support Desk
  useGetSupportTicketsQuery,
  useGetSupportTicketQuery,
  useCreateSupportTicketMutation,
  useUpdateSupportTicketMutation,
  useAddTicketMessageMutation,
  useAssignSupportTicketMutation,
  useCloseSupportTicketMutation,
  // Lazy queries
  useLazyGetEventsQuery,
  useLazyGetEventQuery,
  useLazyGetKanbanBoardQuery,
  useLazyGetSupportTicketQuery,
} = managementApi;