# Role-Based Navigation System

This document describes the implementation of role-based visibility for sitemap and routes in the Orkestra frontend application.

## Overview

The role-based navigation system ensures that users only see navigation items and routes they have permission to access based on their assigned role. This implements a hierarchical role system with four levels:

- `super_admin` - Full system access
- `administrator` - Business operations management
- `manager` - Team and operational management
- `operator` - Field operations and task execution

## Architecture

### 1. Role Hierarchy

Higher roles inherit permissions from lower roles:

```
super_admin (Level 0) - Highest privilege
├─ administrator (Level 1)
├─ manager (Level 2)
└─ operator (Level 3) - Lowest privilege
```

### 2. Core Components

#### Navigation Types (`routes/siteMaps.ts`)
- `NavItem` interface extended with optional `roles[]` and `permissions[]` fields
- `RouteGroup` interface extended with optional `roles[]` and `permissions[]` fields

#### Role Utilities (`utils/roleUtils.ts`)
- `hasRoleAccess()` - Check hierarchical role access
- `hasAnyRole()` - Check if user has any of specified roles
- `extractUserRole()` - Extract role from auth data
- Pre-defined role configurations for common access patterns

#### Navigation Hook (`hooks/useRoleBasedNavigation.ts`)
- `useRoleBasedNavigation()` - Filter navigation based on user role/permissions
- `useCanAccessRoute()` - Check if user can access a specific route
- Memoized filtering for performance

#### Navigation Component Updates
- `NavbarVertical.tsx` - Uses filtered navigation instead of raw routes
- Authentication check prevents rendering for unauthenticated users

## Usage

### 1. Configuring Route Access

Add role requirements to navigation items:

```typescript
// Require specific role
{
  name: 'Admin Dashboard',
  to: '/admin',
  roles: ['super_admin'] // Only super admin
}

// Allow multiple roles
{
  name: 'Reports',
  to: '/reports',
  roles: ['administrator', 'super_admin'] // Admin or super admin
}

// Use hierarchy (recommended)
{
  name: 'Team Management',
  to: '/teams',
  roles: ['manager'] // Manager, administrator, or super admin
}
```

### 2. Group-Level Access Control

Apply access control to entire navigation groups:

```typescript
export const adminRoutes: RouteGroup = {
  label: 'Administration',
  roles: ['administrator'], // Group only visible to administrator+
  children: [
    // All children inherit group access by default
    { name: 'Fleet Management', to: '/fleet' },
    { name: 'Analytics', to: '/analytics' }
  ]
};
```

### 3. Permission-Based Access

Combine roles with permission-based access:

```typescript
{
  name: 'Financial Reports',
  to: '/reports/financial',
  roles: ['administrator'],
  permissions: ['reports:financial:read']
}
```

## Production Route Structure

The system defines four main route groups for production use:

### Operator Routes (`operatorRoutes`)
**Access:** All authenticated users
**Features:**
- Dashboard
- My Tasks
- Calendar
- Profile Management

### Manager Routes (`managerRoutes`)
**Access:** Manager and above
**Features:**
- Task Management (create, assign, oversee)
- Team Overview
- Operational Reports

### Administrator Routes (`adminRoutes`)
**Access:** Administrator and above
**Features:**
- Fleet Management
- Advanced Analytics
- Business Reports
- Financial Data

### Super Admin Routes (`superAdminRoutes`)
**Access:** Super Admin only
**Features:**
- User Management
- Role Management
- System Settings
- System Health

## Development Tools

### Navigation Tester Component
Located at `/test/role-navigation` (super admin only), provides:
- Visual testing of navigation for different roles
- Mock user switching
- Real-time filtering preview

### Test Utilities (`utils/roleNavigationTests.ts`)
- `testNavigationForRole()` - Test filtering for specific role
- `testNavigationForAllRoles()` - Test all roles
- `logNavigationTests()` - Console debugging
- `testRoleHierarchy()` - Verify hierarchy logic

Usage in development:
```javascript
import { runAllNavigationTests } from 'utils/roleNavigationTests';

// Run in browser console
runAllNavigationTests();
```

## Security Considerations

### Defense in Depth
- **UI Filtering:** Hide navigation items users can't access
- **Route Protection:** Use `ProtectedRoute` component for route-level security
- **Backend Validation:** Always validate permissions on API endpoints

### Best Practices
1. **Never rely on frontend-only security** - Always validate on backend
2. **Use hierarchical roles** - Prefer `roles: ['manager']` over explicit role lists
3. **Principle of least privilege** - Grant minimum required access
4. **Regular audits** - Review role assignments periodically

## Integration with Authentication

The system integrates with existing authentication:
- Uses Redux auth store for current user role
- Works with `useAuth()` hook permissions
- Respects session validity checks
- Handles unauthenticated state gracefully

## Environment Configuration

### Development vs Production
- Development: Shows all route groups including development tools
- Production: Only shows production Orkestra route groups
- Test components only render in development mode

### Feature Flags
```typescript
// Development routes only shown in development
const routeGroups = process.env.NODE_ENV === 'development'
  ? [operatorRoutes, managerRoutes, adminRoutes, superAdminRoutes, developmentRoutes]
  : [operatorRoutes, managerRoutes, adminRoutes, superAdminRoutes];
```

## Troubleshooting

### Common Issues

**Navigation not filtering:**
- Check if user role is properly set in Redux store
- Verify role strings match exactly (`'manager'` not `'Manager'`)
- Ensure authentication state is properly loaded

**Empty navigation:**
- Check if user has valid role assignment
- Verify route group roles aren't too restrictive
- Check browser console for role extraction warnings

**Debugging Tools:**
```javascript
// Check current user role
console.log('User role:', useAuth().user?.role);

// Run navigation tests
import { runAllNavigationTests } from 'utils/roleNavigationTests';
runAllNavigationTests();
```

## Future Enhancements

### Planned Features
- Permission-based granular access control
- Dynamic role assignment
- Audit logging for role changes
- Role-based dashboard customization

### Extension Points
- Custom permission validators
- External role providers
- Multi-tenant role isolation
- Advanced caching strategies

---

For questions or issues, refer to the main project documentation or create an issue in the project repository.