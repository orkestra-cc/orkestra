import { baseApi } from './baseApi';

// Chat types
export interface ChatMessage {
  id: string;
  senderId: string;
  receiverId?: string;
  channelId?: string;
  content: string;
  timestamp: string;
  type: 'text' | 'image' | 'file' | 'system';
  status: 'sending' | 'sent' | 'delivered' | 'read' | 'failed';
  metadata?: {
    fileName?: string;
    fileSize?: number;
    imageUrl?: string;
    replyTo?: string;
  };
}

export interface ChatChannel {
  id: string;
  name: string;
  type: 'direct' | 'group' | 'public' | 'private';
  participants: ChatParticipant[];
  lastMessage?: ChatMessage;
  unreadCount: number;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ChatParticipant {
  id: string;
  name: string;
  avatar?: string;
  isOnline: boolean;
  lastSeen?: string;
  role?: 'owner' | 'admin' | 'member';
}

// Email types
export interface Email {
  id: string;
  from: EmailAddress;
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  subject: string;
  content: string;
  htmlContent?: string;
  attachments: EmailAttachment[];
  timestamp: string;
  isRead: boolean;
  isStarred: boolean;
  isImportant: boolean;
  labels: string[];
  folder: 'inbox' | 'sent' | 'drafts' | 'trash' | 'spam';
}

export interface EmailAddress {
  email: string;
  name?: string;
}

export interface EmailAttachment {
  id: string;
  filename: string;
  size: number;
  mimeType: string;
  url?: string;
}

export interface EmailFolder {
  id: string;
  name: string;
  count: number;
  unreadCount: number;
}

// Request/Response types
export interface SendMessageRequest {
  receiverId?: string;
  channelId?: string;
  content: string;
  type?: 'text' | 'image' | 'file';
  metadata?: ChatMessage['metadata'];
}

export interface SendEmailRequest {
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  subject: string;
  content: string;
  htmlContent?: string;
  attachments?: File[];
}

export interface ChatMessagesResponse {
  messages: ChatMessage[];
  hasMore: boolean;
  nextCursor?: string;
}

export interface EmailsResponse {
  emails: Email[];
  hasMore: boolean;
  nextCursor?: string;
  totalCount: number;
}

// Communications API slice
export const communicationsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Chat endpoints
    getChatChannels: builder.query<ChatChannel[], void>({
      query: () => '/chat/channels',
      providesTags: ['Chat'],
      keepUnusedDataFor: 300, // 5 minutes
    }),

    getChatMessages: builder.query<ChatMessagesResponse, {
      channelId?: string;
      receiverId?: string;
      cursor?: string;
      limit?: number;
    }>({
      query: ({ channelId, receiverId, cursor, limit = 50 }) => {
        const params = new URLSearchParams();
        if (channelId) params.append('channelId', channelId);
        if (receiverId) params.append('receiverId', receiverId);
        if (cursor) params.append('cursor', cursor);
        params.append('limit', limit.toString());

        return `/chat/messages?${params.toString()}`;
      },
      providesTags: (_result, _error, { channelId, receiverId }) => [
        'Chat',
        { type: 'Chat', id: channelId || receiverId || 'messages' }
      ],
      serializeQueryArgs: ({ queryArgs }) => {
        // Ensure proper caching based on channel/receiver
        return `messages-${queryArgs.channelId || queryArgs.receiverId || 'default'}`;
      },
      merge: (currentCache, newItems, { arg }) => {
        if (arg.cursor) {
          // Append new messages for pagination
          return {
            ...newItems,
            messages: [...currentCache.messages, ...newItems.messages]
          };
        }
        return newItems;
      },
      forceRefetch: ({ currentArg, previousArg }) => {
        return currentArg?.channelId !== previousArg?.channelId ||
               currentArg?.receiverId !== previousArg?.receiverId;
      },
    }),

    sendMessage: builder.mutation<ChatMessage, SendMessageRequest>({
      query: (message) => ({
        url: '/chat/messages',
        method: 'POST',
        body: message,
      }),
      invalidatesTags: (_result, _error, { channelId, receiverId }) => [
        'Chat',
        { type: 'Chat', id: channelId || receiverId || 'messages' }
      ],
      // Optimistic update
      onQueryStarted: async (message, { dispatch, queryFulfilled }) => {
        const optimisticMessage: ChatMessage = {
          id: `temp-${Date.now()}`,
          senderId: 'current-user', // Should get from auth state
          receiverId: message.receiverId,
          channelId: message.channelId,
          content: message.content,
          timestamp: new Date().toISOString(),
          type: message.type || 'text',
          status: 'sending',
          metadata: message.metadata,
        };

        // Optimistically update the messages cache
        const patchResult = dispatch(
          communicationsApi.util.updateQueryData(
            'getChatMessages',
            { channelId: message.channelId, receiverId: message.receiverId },
            (draft) => {
              draft.messages.unshift(optimisticMessage);
            }
          )
        );

        try {
          const { data: sentMessage } = await queryFulfilled;
          // Replace optimistic message with real message
          dispatch(
            communicationsApi.util.updateQueryData(
              'getChatMessages',
              { channelId: message.channelId, receiverId: message.receiverId },
              (draft) => {
                const index = draft.messages.findIndex(m => m.id === optimisticMessage.id);
                if (index >= 0) {
                  draft.messages[index] = sentMessage;
                }
              }
            )
          );
        } catch {
          // Revert optimistic update on failure
          patchResult.undo();
        }
      },
    }),

    markMessagesRead: builder.mutation<void, { channelId?: string; receiverId?: string; messageIds: string[] }>({
      query: ({ channelId, receiverId, messageIds }) => ({
        url: '/chat/messages/read',
        method: 'POST',
        body: { channelId, receiverId, messageIds },
      }),
      invalidatesTags: ['Chat'],
    }),

    // Email endpoints
    getEmailFolders: builder.query<EmailFolder[], void>({
      query: () => '/email/folders',
      providesTags: ['Email'],
      keepUnusedDataFor: 300,
    }),

    getEmails: builder.query<EmailsResponse, {
      folder?: string;
      cursor?: string;
      limit?: number;
      search?: string;
    }>({
      query: ({ folder = 'inbox', cursor, limit = 25, search }) => {
        const params = new URLSearchParams();
        params.append('folder', folder);
        if (cursor) params.append('cursor', cursor);
        params.append('limit', limit.toString());
        if (search) params.append('search', search);

        return `/email/messages?${params.toString()}`;
      },
      providesTags: (_result, _error, { folder }) => [
        'Email',
        { type: 'Email', id: folder || 'inbox' }
      ],
      serializeQueryArgs: ({ queryArgs }) => {
        return `emails-${queryArgs.folder || 'inbox'}-${queryArgs.search || ''}`;
      },
      merge: (currentCache, newItems, { arg }) => {
        if (arg.cursor) {
          return {
            ...newItems,
            emails: [...currentCache.emails, ...newItems.emails]
          };
        }
        return newItems;
      },
    }),

    getEmail: builder.query<Email, string>({
      query: (emailId) => `/email/messages/${emailId}`,
      providesTags: (_result, _error, emailId) => [
        { type: 'Email', id: emailId }
      ],
    }),

    sendEmail: builder.mutation<Email, SendEmailRequest>({
      query: (email) => ({
        url: '/email/send',
        method: 'POST',
        body: email,
      }),
      invalidatesTags: ['Email', { type: 'Email', id: 'sent' }],
    }),

    markEmailRead: builder.mutation<void, { emailId: string; isRead: boolean }>({
      query: ({ emailId, isRead }) => ({
        url: `/email/messages/${emailId}/read`,
        method: 'PUT',
        body: { isRead },
      }),
      invalidatesTags: (_result, _error, { emailId }) => [
        'Email',
        { type: 'Email', id: emailId }
      ],
    }),

    markEmailStarred: builder.mutation<void, { emailId: string; isStarred: boolean }>({
      query: ({ emailId, isStarred }) => ({
        url: `/email/messages/${emailId}/star`,
        method: 'PUT',
        body: { isStarred },
      }),
      invalidatesTags: (_result, _error, { emailId }) => [
        'Email',
        { type: 'Email', id: emailId }
      ],
    }),

    deleteEmail: builder.mutation<void, string>({
      query: (emailId) => ({
        url: `/email/messages/${emailId}`,
        method: 'DELETE',
      }),
      invalidatesTags: (_result, _error, emailId) => [
        'Email',
        { type: 'Email', id: emailId }
      ],
    }),

    moveEmail: builder.mutation<void, { emailId: string; folder: string }>({
      query: ({ emailId, folder }) => ({
        url: `/email/messages/${emailId}/move`,
        method: 'PUT',
        body: { folder },
      }),
      invalidatesTags: ['Email'],
    }),
  }),
});

// Export hooks
export const {
  // Chat hooks
  useGetChatChannelsQuery,
  useGetChatMessagesQuery,
  useSendMessageMutation,
  useMarkMessagesReadMutation,
  // Email hooks
  useGetEmailFoldersQuery,
  useGetEmailsQuery,
  useGetEmailQuery,
  useSendEmailMutation,
  useMarkEmailReadMutation,
  useMarkEmailStarredMutation,
  useDeleteEmailMutation,
  useMoveEmailMutation,
  // Lazy queries
  useLazyGetChatMessagesQuery,
  useLazyGetEmailsQuery,
  useLazyGetEmailQuery,
} = communicationsApi;