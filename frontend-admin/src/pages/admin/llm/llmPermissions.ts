import { useAuth } from 'hooks/auth/useAuthRTK';

// The literal keys of backend/internal/core/llm/module.go Permissions().
// Every mutation is gated on the permission of ITS resource: managing a
// model does not grant deciding who may use it (spec RBAC). Access
// (granted/everyone) and the grant list are both set on the grants route,
// so both need llm.grants.admin.
export const LLM_PERMISSIONS = {
  read: 'llm.admin.read',
  credentials: 'llm.credentials.admin',
  models: 'llm.models.admin',
  grants: 'llm.grants.admin'
} as const;

export const useLlmPermissions = () => {
  const { hasPermission } = useAuth();
  const P = LLM_PERMISSIONS;
  return {
    canRead: hasPermission(P.read),
    canManageCredentials: hasPermission(P.credentials),
    canManageModels: hasPermission(P.models),
    canManageGrants: hasPermission(P.grants)
  };
};
