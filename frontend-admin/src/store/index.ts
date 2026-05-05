import { configureStore, combineReducers } from '@reduxjs/toolkit';
import { persistStore, persistReducer, FLUSH, REHYDRATE, PAUSE, PERSIST, PURGE, REGISTER } from 'redux-persist';
import storage from 'redux-persist/lib/storage';

import authReducer from './slices/authSlice';
import kanbanReducer from './slices/kanbanSlice';
import tenantReducer from './slices/tenantSlice';
import { baseApi } from './api/baseApi';

const authPersistConfig = {
  key: 'falcon-auth-storage',
  storage,
  whitelist: ['preferences']
};

const rootReducer = combineReducers({
  auth: persistReducer(authPersistConfig, authReducer),
  tenant: tenantReducer,
  kanban: kanbanReducer,
  // Add RTK Query API slice
  [baseApi.reducerPath]: baseApi.reducer,
});

const persistedReducer = persistReducer(
  {
    key: 'root',
    storage,
    // Don't persist auth, tenant, kanban, or RTK Query cache
    blacklist: ['auth', 'tenant', 'kanban', baseApi.reducerPath]
  },
  rootReducer
);

export const store = configureStore({
  reducer: persistedReducer,
  middleware: (getDefaultMiddleware) => {
    const middleware = getDefaultMiddleware({
      serializableCheck: {
        ignoredActions: [FLUSH, REHYDRATE, PAUSE, PERSIST, PURGE, REGISTER],
        ignoredPaths: ['auth.sessionExpiry']
      },
    })
      // Add RTK Query middleware
      .concat(baseApi.middleware);

    return middleware;
  },
  devTools: process.env.NODE_ENV === 'development' && {
    name: 'Orkestra Redux Store',
    maxAge: 50,
    serialize: {
      options: {
        undefined: true,
        function: true,
        symbol: true,
      },
    },
    actionSanitizer: (action: any) => {
      return action;
    },
    stateSanitizer: (state: any) => {
      if (state.auth) {
        return {
          ...state,
          auth: {
            ...state.auth,
            accessToken: state.auth.accessToken ? '[REDACTED]' : null
          }
        };
      }
      return state;
    }
  },
});

export const persistor = persistStore(store);

export type RootState = ReturnType<typeof store.getState>;
export type AppDispatch = typeof store.dispatch;

// Development helpers
if (process.env.NODE_ENV === 'development') {
  (window as any).__store = store;
  (window as any).__persistor = persistor;

  // Store helpers for debugging
  (window as any).__storeHelpers = {
    getState: () => store.getState(),
    dispatch: (action: any) => store.dispatch(action),
    resetAuth: () => store.dispatch({ type: 'auth/resetAuthState' }),
    resetKanban: () => store.dispatch({ type: 'kanban/resetKanbanState' }),
    clearPersistedState: () => {
      persistor.purge();
      window.location.reload();
    }
  };

  // Test auth helper
  (window as any).__testEveAuth = () => {
    const testUser = {
      characterId: 123456789,
      characterName: 'Test Character',
      userId: 1,
      email: 'test@example.com',
      permissions: ['read', 'write', 'admin']
    };

    store.dispatch({
      type: 'auth/login',
      payload: {
        userData: testUser
      }
    });

    console.log('✅ Test authentication applied');
  };
}