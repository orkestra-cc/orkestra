# Redux Auth Integration Guide

## ✅ Implementation Complete

The auth/me API response is now properly stored in Redux for consistent state management across the application. This guide explains the complete integration architecture.

## 🏗️ Architecture Overview

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   API Endpoint  │───▶│  TanStack Query  │───▶│     Redux       │
│  /api/v1/auth/me│    │   (API Client)   │    │  (State Store)  │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                        │
                                │                        │
                                ▼                        ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │  AuthSyncProvider │    │  Role-Based     │
                       │    (Bridge)      │    │  Navigation     │
                       └──────────────────┘    └─────────────────┘
```

### Data Flow
1. **TanStack Query** fetches user data from `/api/v1/auth/me`
2. **AuthSyncProvider** bridges TanStack Query data to Redux
3. **Redux** stores the user data for consistent app-wide state
4. **Navigation & Components** use Redux auth state

## 🔧 Integration Components

### 1. **Redux Auth Slice** (`store/slices/authSlice.ts`)
Enhanced to handle auth/me response:

```typescript
// New action to save API response as-is
setUserFromApiResponse: (state, action: PayloadAction<User>) => {
  const userData = action.payload;

  if (userData && userData.isActive) {
    // Save complete API response directly
    state.user = userData; // Complete user data preserved
    state.isAuthenticated = true;
    state.permissions = [userData.role]; // Simple role-based permissions
  } else {
    // Clear state for unauthenticated users
    Object.assign(state, initialState);
  }
}
```

### 2. **Auth Bridge Provider** (`components/providers/AuthSyncProvider.tsx`)
Syncs TanStack Query auth data to Redux:

```typescript
const AuthSyncProvider = ({ children }) => {
  const { user: apiUser, isAuthenticated, isLoading, error } = useCurrentUser();
  const { setUserFromApiResponse, setLoading, setError, logout } = useAuth();

  useEffect(() => {
    if (isLoading) {
      setLoading(true);
    } else if (error) {
      setError(error.message);
    } else if (isAuthenticated && apiUser) {
      setUserFromApiResponse(apiUser); // Save complete API response to Redux
    } else {
      logout(); // Clear Redux state
    }
  }, [apiUser, isAuthenticated, isLoading, error]);

  return <>{children}</>;
};
```

### 3. **Enhanced Redux Auth Hook** (`hooks/redux/useAuth.ts`)
Added new action for API data sync:

```typescript
const setUserFromApi = useCallback((authData: any) => {
  dispatch(setUserFromApiResponse(authData));
}, [dispatch]);

return {
  // ... existing actions
  setUserFromApiResponse: setUserFromApi,
  // ... utility functions
};
```

### 4. **Role-Based Navigation** (`hooks/useRoleBasedNavigation.ts`)
Now uses Redux auth state consistently:

```typescript
const { user, isAuthenticated, hasPermission, hasAnyPermission } = useAuth(); // Redux
```

## 🎯 Usage in Your App

### 1. **Setup the Bridge Provider**
Wrap your app with the AuthSyncProvider to enable auto-sync:

```typescript
// In your main App component or provider setup
import AuthSyncProvider from 'components/providers/AuthSyncProvider';

function App() {
  return (
    <ReduxProvider>
      <AuthSyncProvider>
        <YourAppComponents />
      </AuthSyncProvider>
    </ReduxProvider>
  );
}
```

### 2. **Using Auth Data in Components**
Always use Redux auth state for consistency:

```typescript
// ✅ CORRECT - Use Redux auth
import { useAuth } from 'hooks/redux/useAuth';

function MyComponent() {
  const { user, isAuthenticated, isLoading } = useAuth();

  if (isAuthenticated && user?.role === 'manager') {
    // User is manager, show manager features
  }
}

// ❌ AVOID - Don't use TanStack Query auth directly in components
import { useCurrentUser } from 'hooks/auth/useAuth'; // Only use for debugging
```

### 3. **Role-Based Access Control**
The system automatically extracts roles from the API response:

```typescript
// API Response Structure (what you should return from /api/v1/auth/me):
{
  "isActive": true,
  "id": "user_123",
  "fullName": "John Manager",
  "email": "john@company.com",
  "role": "manager", // ← This gets stored in Redux
  // ... other fields
}

// In your components:
const { user } = useAuth();
const userRole = user?.role; // "manager"
```

## 🛠️ Development Tools

### 1. **Role Navigation Tester** (`/test/role-navigation`)
Updated to work with Redux auth:
- Shows real user role from Redux
- Tests navigation filtering based on Redux auth state
- Compares mock scenarios with actual user data

## 🔄 Auto-Sync Behavior

The AuthSyncProvider automatically:

1. **On API Loading**: Sets Redux loading state
2. **On API Success**: Updates Redux with user data from auth/me
3. **On API Error**: Sets Redux error state
4. **On Logout**: Clears Redux auth state
5. **Real-time Updates**: Keeps Redux in sync with API changes

## 📋 API Response Requirements

Your `/api/v1/auth/me` endpoint must return:

```json
{
  "isActive": true,        // Required: Authentication status
  "id": "string",          // Required: User ID
  "fullName": "string",    // Required: Display name
  "email": "string",       // Optional: User email
  "role": "manager",       // Required: One of: super_admin, administrator, manager, operator
  // ... any other user data
}
```

**Important**: The `role` field is critical for role-based navigation to work.

## 🧪 Testing the Integration

### 1. **Test Navigation**
- ✅ Navigation items appear/disappear based on user role
- ✅ Higher roles can access lower role features
- ✅ Role changes update navigation immediately

### 3. **Console Monitoring**
The AuthSyncProvider logs sync activity in development:
```javascript
// Look for logs like:
🔄 Auth Sync: {
  apiAuthenticated: true,
  apiLoading: false,
  apiUser: { role: "manager", id: "123" },
  apiError: null
}
```

## 🚨 Common Issues & Solutions

### **Navigation not updating**
- Check that AuthSyncProvider is properly wrapped around your app
- Verify `/api/v1/auth/me` returns the `role` field
- Use Auth Debugger to check sync status

### **Role extraction failing**
- Ensure API returns `role` field with valid values
- Check browser console for role extraction warnings
- Verify role value matches: `super_admin`, `administrator`, `manager`, `operator`

### **State inconsistencies**
- AuthSyncProvider should automatically resolve most sync issues
- Check if multiple auth hooks are being used inconsistently
- Use Redux DevTools to inspect auth state changes

## ✨ Benefits of This Architecture

1. **Consistent State**: Single source of truth in Redux
2. **Automatic Sync**: No manual data synchronization required
3. **Performance**: Redux state is cached and efficient
4. **Developer Experience**: Clear debugging tools and consistent APIs
5. **Type Safety**: Full TypeScript integration
6. **Flexibility**: Easy to extend with additional auth features

The integration is now complete and your auth/me response data is properly stored and managed in Redux! 🎉