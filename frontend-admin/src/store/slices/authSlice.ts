import { createSlice, PayloadAction, createAsyncThunk } from '@reduxjs/toolkit';
import type { RootState } from '../index';

// Import backend user types from authApi
import type { BackendUser } from '../api/authApi';

// Extended User interface for Redux state management (adds permissions)
interface User extends BackendUser {
  permissions?: string[]; // Added for client-side RBAC
}

interface UserPreferences {
  theme: 'light' | 'dark';
  language: string;
  notifications: boolean;
}

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: string | null;
  sessionExpiry: string | null; // ISO date string
  permissions: string[];
  preferences: UserPreferences;
  _isLoggingOut: boolean;
  accessToken: string | null;
  tokenExpiry: string | null; // ISO date string
}

interface LoginPayload {
  userData: BackendUser; // Use proper backend user type
}

const initialState: AuthState = {
  user: null,
  isAuthenticated: false,
  isLoading: true, // Start with loading true - session check will determine actual state
  error: null,
  sessionExpiry: null,
  permissions: [],
  preferences: {
    theme: 'light',
    language: 'en',
    notifications: true
  },
  _isLoggingOut: false,
  accessToken: null,
  tokenExpiry: null
};

export const refreshSession = createAsyncThunk(
  'auth/refreshSession',
  async () => {
    try {
      const response = await fetch('/v1/auth/operator/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include' // Use HttpOnly cookies exclusively
      });

      if (!response.ok) {
        throw new Error('Session refresh failed');
      }

      const data = await response.json();
      return data;
    } catch (error) {
      throw error;
    }
  }
);

const authSlice = createSlice({
  name: 'auth',
  initialState,
  reducers: {
    login: (state, action: PayloadAction<LoginPayload>) => {
      const { userData } = action.payload;
      state.user = {
        ...userData,
        permissions: userData.role ? [userData.role] : []
      };
      state.isAuthenticated = true;
      state.isLoading = false;
      state.error = null;
      state.sessionExpiry = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
      state.permissions = userData.role ? [userData.role] : [];
    },

    logout: (state) => {
      state._isLoggingOut = true;
      state.user = null;
      state.isAuthenticated = false;
      state.isLoading = false;
      state.error = null;
      state.sessionExpiry = null;
      state.permissions = [];
      state._isLoggingOut = false;
    },

    setLoading: (state, action: PayloadAction<boolean>) => {
      state.isLoading = action.payload;
    },

    setError: (state, action: PayloadAction<string | null>) => {
      state.error = action.payload;
      state.isLoading = false;
    },

    clearError: (state) => {
      state.error = null;
    },

    updateSession: (state) => {
      // Session updated via HttpOnly cookies - just update expiry
      state.sessionExpiry = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
    },

    updateUser: (state, action: PayloadAction<Partial<BackendUser>>) => {
      if (state.user) {
        state.user = { ...state.user, ...action.payload };
        // Update permissions if role changed
        if (action.payload.role) {
          const newPermissions = [action.payload.role];
          state.permissions = newPermissions;
          state.user.permissions = newPermissions;
        }
      }
    },

    updatePermissions: (state, action: PayloadAction<string[]>) => {
      state.permissions = action.payload;
      if (state.user) {
        state.user.permissions = action.payload;
      }
    },

    updatePreferences: (state, action: PayloadAction<Partial<UserPreferences>>) => {
      state.preferences = { ...state.preferences, ...action.payload };
    },

    resetAuthState: (state) => {
      Object.assign(state, initialState);
    },

    checkSessionExpiry: (state) => {
      if (state.sessionExpiry) {
        const now = new Date();
        const expiry = new Date(state.sessionExpiry);
        if (now >= expiry) {
          Object.assign(state, initialState);
        }
      }
    },

    // Save /auth/me API response directly as Redux state
    setUserFromApiResponse: (state, action: PayloadAction<BackendUser | null>) => {
      const userData = action.payload;

      if (userData && userData.isActive) {
        // User is authenticated, save API response directly
        state.user = {
          ...userData,
          permissions: userData.role ? [userData.role] : []
        };
        state.isAuthenticated = true;
        state.isLoading = false;
        state.error = null;
        state.sessionExpiry = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();

        // Set permissions based on role (simplified RBAC)
        state.permissions = userData.role ? [userData.role] : [];
      } else {
        // User is not authenticated or data is null, clear state
        Object.assign(state, {
          ...initialState,
          isLoading: false
        });
      }
    },

    // Set access token from /auth/session response
    setAccessToken: (state, action: PayloadAction<{ accessToken: string; expiresIn: number }>) => {
      state.accessToken = action.payload.accessToken;
      state.tokenExpiry = new Date(Date.now() + action.payload.expiresIn * 1000).toISOString();
    },

    // Clear access token
    clearAccessToken: (state) => {
      state.accessToken = null;
      state.tokenExpiry = null;
    }
  },
  extraReducers: (builder) => {
    builder
      .addCase(refreshSession.pending, (state) => {
        state.isLoading = true;
      })
      .addCase(refreshSession.fulfilled, (state) => {
        // Session refreshed via HttpOnly cookies - just update expiry
        state.sessionExpiry = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
        state.isLoading = false;
      })
      .addCase(refreshSession.rejected, (state) => {
        Object.assign(state, initialState);
        state.isLoading = false;
      });
  }
});

export const {
  login,
  logout,
  setLoading,
  setError,
  clearError,
  updateSession,
  updateUser,
  updatePermissions,
  updatePreferences,
  resetAuthState,
  checkSessionExpiry,
  setUserFromApiResponse,
  setAccessToken,
  clearAccessToken
} = authSlice.actions;

export const selectAuth = (state: RootState) => state.auth;
export const selectUser = (state: RootState) => state.auth.user;
export const selectIsAuthenticated = (state: RootState) => state.auth.isAuthenticated;
export const selectIsLoading = (state: RootState) => state.auth.isLoading;
export const selectAuthError = (state: RootState) => state.auth.error;
export const selectPermissions = (state: RootState) => state.auth.permissions;
export const selectPreferences = (state: RootState) => state.auth.preferences;

export const selectHasPermission = (permission: string) => (state: RootState) => {
  return state.auth.permissions.includes(permission);
};

export const selectHasAnyPermission = (permissions: string[]) => (state: RootState) => {
  return permissions.some(p => state.auth.permissions.includes(p));
};

export const selectIsSessionValid = (state: RootState) => {
  if (!state.auth.sessionExpiry) return false;
  return new Date() < new Date(state.auth.sessionExpiry);
};

export const selectUserDisplayName = (state: RootState) => {
  const user = state.auth.user;
  if (!user) return 'Guest';
  return user.fullName || user.email || user.username || 'User';
};

// Additional selectors for new fields
export const selectUserAvatar = (state: RootState) => {
  return state.auth.user?.avatar;
};

export const selectUserUsername = (state: RootState) => {
  return state.auth.user?.username;
};

export const selectUserEmailVerified = (state: RootState) => {
  return state.auth.user?.emailVerified || false;
};

export const selectUserOAuthProviders = (state: RootState) => {
  return state.auth.user?.oauthProviders || [];
};

export const selectUserLastLogin = (state: RootState) => {
  return state.auth.user?.lastLogin ? new Date(state.auth.user.lastLogin) : null;
};

export const selectUserCreatedAt = (state: RootState) => {
  return state.auth.user?.createdAt ? new Date(state.auth.user.createdAt) : null;
};

export const selectAccessToken = (state: RootState) => state.auth.accessToken;

export const selectTokenExpiry = (state: RootState) => state.auth.tokenExpiry;

export default authSlice.reducer;