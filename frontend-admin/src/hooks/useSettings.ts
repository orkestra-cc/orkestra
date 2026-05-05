import { baseApi } from '../store/api/baseApi';

// Settings types
export interface UserSettings {
  id: string;
  userId: string;
  theme: 'light' | 'dark' | 'auto';
  language: string;
  notifications: {
    email: boolean;
    push: boolean;
    desktop: boolean;
  };
  preferences: {
    dateFormat: string;
    timeFormat: '12h' | '24h';
    timezone: string;
  };
  privacy: {
    profileVisibility: 'public' | 'private' | 'friends';
    showEmail: boolean;
    showLastActive: boolean;
  };
  createdAt: string;
  updatedAt: string;
}

export interface UpdateSettingsRequest {
  theme?: UserSettings['theme'];
  language?: string;
  notifications?: Partial<UserSettings['notifications']>;
  preferences?: Partial<UserSettings['preferences']>;
  privacy?: Partial<UserSettings['privacy']>;
}

// Settings API slice
export const settingsApi = baseApi.injectEndpoints({
  endpoints: (builder) => ({
    // Get user settings
    getUserSettings: builder.query<UserSettings, void>({
      query: () => '/user/settings',
      providesTags: ['User'],
      keepUnusedDataFor: 600, // 10 minutes
    }),

    // Update user settings
    updateUserSettings: builder.mutation<UserSettings, UpdateSettingsRequest>({
      query: (updates) => ({
        url: '/user/settings',
        method: 'PUT',
        body: updates,
      }),
      invalidatesTags: ['User'],
      // Optimistic update
      onQueryStarted: async (updates, { dispatch, queryFulfilled }) => {
        const patchResult = dispatch(
          settingsApi.util.updateQueryData('getUserSettings', undefined, (draft) => {
            Object.assign(draft, updates);
          })
        );

        try {
          await queryFulfilled;
        } catch {
          patchResult.undo();
        }
      },
    }),

    // Reset settings to defaults
    resetUserSettings: builder.mutation<UserSettings, void>({
      query: () => ({
        url: '/user/settings/reset',
        method: 'POST',
      }),
      invalidatesTags: ['User'],
    }),

    // Export user settings
    exportUserSettings: builder.query<Blob, void>({
      query: () => ({
        url: '/user/settings/export',
        responseHandler: (response) => response.blob(),
      }),
      // Don't cache exports
      keepUnusedDataFor: 0,
    }),

    // Import user settings
    importUserSettings: builder.mutation<UserSettings, File>({
      query: (file) => {
        const formData = new FormData();
        formData.append('settings', file);
        return {
          url: '/user/settings/import',
          method: 'POST',
          body: formData,
        };
      },
      invalidatesTags: ['User'],
    }),
  }),
});

// Export hooks
export const {
  useGetUserSettingsQuery,
  useUpdateUserSettingsMutation,
  useResetUserSettingsMutation,
  useLazyExportUserSettingsQuery,
  useImportUserSettingsMutation,
} = settingsApi;

// Enhanced settings hook with common operations
export const useSettings = () => {
  const {
    data: settings,
    isLoading,
    error,
    refetch
  } = useGetUserSettingsQuery();

  const [updateSettings, { isLoading: isUpdating }] = useUpdateUserSettingsMutation();
  const [resetSettings, { isLoading: isResetting }] = useResetUserSettingsMutation();
  const [importSettings, { isLoading: isImporting }] = useImportUserSettingsMutation();

  // Helper functions for common settings operations
  const updateTheme = async (theme: UserSettings['theme']) => {
    try {
      await updateSettings({ theme }).unwrap();
    } catch (error) {
      console.error('Failed to update theme:', error);
      throw error;
    }
  };

  const updateNotifications = async (notifications: Partial<UserSettings['notifications']>) => {
    try {
      await updateSettings({ notifications }).unwrap();
    } catch (error) {
      console.error('Failed to update notifications:', error);
      throw error;
    }
  };

  const updatePreferences = async (preferences: Partial<UserSettings['preferences']>) => {
    try {
      await updateSettings({ preferences }).unwrap();
    } catch (error) {
      console.error('Failed to update preferences:', error);
      throw error;
    }
  };

  const updatePrivacy = async (privacy: Partial<UserSettings['privacy']>) => {
    try {
      await updateSettings({ privacy }).unwrap();
    } catch (error) {
      console.error('Failed to update privacy settings:', error);
      throw error;
    }
  };

  return {
    // Data
    settings,
    isLoading,
    error,

    // Loading states
    isUpdating,
    isResetting,
    isImporting,

    // Actions
    updateSettings,
    resetSettings,
    importSettings,
    refetch,

    // Helper functions
    updateTheme,
    updateNotifications,
    updatePreferences,
    updatePrivacy,
  };
};