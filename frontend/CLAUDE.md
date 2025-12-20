# Module: Frontend - React Web Application
*Path: `/frontend`*
*Parent: [../CLAUDE.md](../CLAUDE.md)*

<!-- Navigation -->
[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)
<!-- /Navigation -->

## Module Purpose

The frontend serves as the **React-based web application** providing comprehensive admin dashboards and management interfaces for the Orkestra system.

- **Primary Role**: Web-based user interface for administrators and managers
- **System Integration**: Consumes backend APIs and WebSocket events for real-time updates
- **Architecture**: Modern React 19 application with TypeScript, state management, and responsive design

## Dependencies

### Imports
- **[`/backend/`](../backend/CLAUDE.md)** - REST APIs, WebSocket events, authentication
- **[`/shared/`](../shared/CLAUDE.md)** - TypeScript types and validation schemas

### Importers
- **End Users**: Web browsers accessing the dashboard interface

## Architecture & Technology Stack

### Core Technologies
- **React 19.1.0** - Latest React with functional components and hooks
- **Vite 7.0.5** - Fast build tool and dev server
- **React Bootstrap 2.10.10** - UI component library
- **Bootstrap 5.3.7** - CSS framework
- **React Router 7.7.0** - Client-side routing
- **SCSS** - Enhanced CSS with variables and nesting

### Key Libraries
- **State Management**: Redux Toolkit with React Redux, Redux Persist
- **Data Fetching**: TanStack Query (React Query) for server state management
- **Forms**: React Hook Form + Yup validation
- **Charts**: ECharts, Chart.js, D3.js
- **Maps**: Google Maps API, Leaflet
- **Date/Time**: Day.js, React DatePicker, FullCalendar
- **Rich Text**: TinyMCE editor
- **Drag & Drop**: DND Kit for Kanban boards
- **Animations**: Lottie React

## Project Structure

```
src/
├── components/           # 🎯 Reusable UI Components ONLY
│   ├── authentication/ # Auth components & layouts (with barrel export)
│   ├── common/         # Reusable UI component library (with barrel export)
│   ├── dashboards/     # Dashboard widget components (with barrel export)
│   ├── navbar/         # Navigation components
│   ├── wizard/         # Form wizard components
│   ├── errors/         # Error page components
│   └── notification/   # Notification components
├── features/           # 🚀 Complete Application Modules
│   ├── chat/          # Full chat application
│   ├── email/         # Complete email client
│   ├── kanban/        # Kanban board system
│   ├── events/        # Event management system
│   ├── social/        # Social media features
│   ├── support-desk/  # Help desk application
│   └── calendar/      # Calendar application
├── demos/             # 🚀 Complete Dashboard Demos
│   └── dashboards/    # Full dashboard layout examples
│       ├── DefaultDashboard.jsx
│       ├── AnalyticsDashboard.jsx
│       ├── CrmDashboard.jsx
│       └── [others...]
├── pages/             # 📄 Page-Level Components
│   ├── faq/          # FAQ pages
│   ├── pricing/      # Pricing pages
│   ├── user/         # User profile pages
│   ├── landing/      # Landing pages
│   └── miscellaneous/ # Other page components
├── docs/              # 📚 Documentation & Examples
│   ├── components/   # Component documentation & examples
│   ├── documentation/ # Development guides & docs
│   └── utilities/    # Bootstrap utility class examples
├── layouts/          # Layout components (9 different layouts)
├── store/           # Redux store configuration and slices
├── providers/        # Context providers and Redux integration
├── routes/          # Routing configuration
├── data/            # Static data and mock APIs
├── hooks/           # Custom React hooks
├── helpers/         # Utility functions
├── assets/          # Images, icons, SCSS files
└── reducers/        # State reducers
```

## Component Organization

### Perfect Separation of Concerns
The project now maintains crystal clear boundaries between different types of code:

#### 🎯 Reusable UI Components (`src/components/`)
**Only truly reusable UI components belong here:**
- **`common/`** - Core UI component library (Avatar, Button, Card, etc.) with barrel export
- **`authentication/`** - Auth-specific components (login forms, protected routes) with barrel export  
- **`dashboards/`** - Reusable dashboard widgets (WeeklySales, ActiveUsers, etc.) with barrel export
- **`navbar/`** - Navigation components (top nav, vertical nav, dropdowns)
- **`wizard/`** - Form wizard components
- **`errors/`** - Error page components (404, 500)
- **`notification/`** - Notification system components

#### 🚀 Complete Application Features (`src/features/`)
**Full-featured application modules with their own state and business logic:**
- **`chat/`** - Complete real-time messaging system
- **`email/`** - Full email client (inbox, compose, detail views)
- **`kanban/`** - Project management boards with drag & drop
- **`events/`** - Event management system (create, list, detail)
- **`social/`** - Social media features (feed, followers, activity log)  
- **`support-desk/`** - Help desk system (tickets, contacts, reports)
- **`calendar/`** - Calendar application with scheduling

#### 🚀 Dashboard Demos (`src/demos/dashboards/`)
**Complete dashboard layouts showing how to compose components:**
- **`DefaultDashboard.jsx`** - General business metrics layout
- **`AnalyticsDashboard.jsx`** - Web analytics dashboard
- **`CrmDashboard.jsx`** - CRM and sales dashboard
- **`ProjectManagementDashboard.jsx`** - Team collaboration dashboard
- **`SaasDashboard.jsx`** - SaaS metrics dashboard
- **`SupportDeskDashboard.jsx`** - Support desk dashboard

#### 📄 Page Components (`src/pages/`)
**Page-level components for routing:**
- **`admin/`** - Administrative pages (SettingsAdmin, SchedulerAdmin)
- **`faq/`** - FAQ pages (basic, accordion, alt layouts)
- **`pricing/`** - Pricing pages (default, alternative layouts)
- **`user/`** - User profile and settings pages
- **`landing/`** - Marketing and landing pages
- **`miscellaneous/`** - Other standalone pages

#### 📚 Documentation (`src/docs/`)
**Documentation, examples, and guides separate from application code:**
- **`components/`** - Component documentation with interactive examples
- **`documentation/`** - Development guides, setup docs, changelogs
- **`utilities/`** - Bootstrap utility class examples and demonstrations

### Layout System
- **MainLayout** - Primary dashboard layout
- **VerticalNavLayout** - Sidebar navigation (default)
- **TopNavLayout** - Top navigation bar
- **ComboNavLayout** - Combined top + sidebar
- **Auth Layouts** - Simple, Card, Split, Wizard variations

## State Management

### Redux Toolkit Architecture

**Primary State Management:** Redux Toolkit with React Redux for predictable state management across the application.

#### Store Structure (`src/store/`)
```
store/
├── index.ts                 # Store configuration with middleware
├── ReduxProvider.tsx        # Provider wrapper with persistence
├── hooks.ts                 # Typed Redux hooks (useAppSelector, useAppDispatch)
└── slices/
    ├── authSlice.ts        # Authentication state management
    └── kanbanSlice.ts      # Kanban board state management
```

#### Redux Slices
- **Auth Slice** - Complete authentication state with session management, user data, permissions, and preferences
- **Kanban Slice** - Kanban board state with drag-and-drop optimization, task management, and UI state

#### Key Features
- **Redux DevTools** - Enhanced debugging with action sanitization and state filtering
- **Redux Persist** - Selective state persistence (only user preferences, not sensitive auth data)
- **Type Safety** - Full TypeScript integration with typed hooks and selectors
- **Development Helpers** - Browser console utilities for debugging and testing

### Context Providers (src/providers/)
- **AppProvider** - Global app configuration (theme, navbar, RTL)
- **AuthProvider** - Authentication state bridge between Redux and React Query
- **KanbanProvider** - Kanban state bridge providing Redux hooks through context
- **ChatProvider** - Chat application state
- **EmailProvider** - Email client state

### Custom Redux Hooks (`src/hooks/redux/`)
- **useAuth** - Complete auth operations with actions and selectors
- **useKanban** - Kanban operations with optimized drag-and-drop actions
- **Component-specific hooks** - Granular hooks for specific state slices

**Key Principles:**
1. **Redux for Complex Shared State**
   - Use Redux for state that needs to be shared across multiple components
   - Auth, Kanban, and other complex feature state managed in Redux

2. **Choose the Right Tool:**
   - **Local State** (`useState`) - Simple component-level state, form inputs, toggles
   - **Redux State** - Complex shared state, authentication, feature-specific state
   - **Context Providers** - Global configuration, dependency injection, bridging Redux with other systems

3. **State Persistence Strategy:**
   - **Auth**: Only user preferences persisted (theme, language, notifications)
   - **Sensitive Data**: Tokens and user data rely on backend session validation
   - **UI State**: Not persisted, recreated on app load

4. **Development Workflow:**
   - Use Redux DevTools for state inspection and time-travel debugging
   - Type-safe development with TypeScript integration

**Current Architecture:** Redux Toolkit provides the foundation for complex state management, with Context providers handling configuration and dependency injection.

## Data Fetching with TanStack Query

### Overview
TanStack Query (React Query) v5 is integrated for efficient server state management, providing powerful caching, synchronization, and background updates for API calls.

### Backend API Specification
All API queries and mutations must respect the backend OpenAPI specification:
- **OpenAPI Spec URL**: https://erpb.blacklab.cc/openapi.json
- **Compliance Required**: All data fetching operations must follow the defined endpoints, request/response schemas, and authentication requirements
- **Schema Validation**: Ensure request payloads and response handling match the OpenAPI definitions
- **Authentication Method**: All API calls use cookie-based authentication with `credentials: 'include'` (NOT Bearer tokens)

### Core Features
- **Intelligent Caching** - Automatic caching with configurable stale times
- **Background Updates** - Keep data fresh with background refetching
- **Optimistic Updates** - Update UI immediately, rollback on failure
- **Infinite Queries** - Built-in infinite scrolling support
- **DevTools Integration** - Debugging tools for development
- **Error Handling** - Robust error handling and retry logic

### Module-Specific Guidelines

- **API Integration**: All API calls must comply with backend OpenAPI specification
- **Authentication**: Use cookie-based authentication with `credentials: 'include'`
- **State Management**: Use Redux Toolkit for complex shared state, React Query for server state
- **Component Organization**: Maintain clear separation between reusable components and features
- **Performance**: Optimize for responsive design and fast loading times
- **Testing**: Comprehensive unit and integration tests for all components
- **Accessibility**: Support screen readers and keyboard navigation

---

### Related Guides
- [Project Overview](../CLAUDE.md) - System architecture and design principles
- [Backend APIs](../backend/CLAUDE.md) - API specifications and authentication
- [Shared Types](../shared/CLAUDE.md) - TypeScript types and validation schemas
- [Docker Development](../docker/CLAUDE.md) - Development environment setup
