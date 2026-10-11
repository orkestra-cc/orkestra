import { useAuth } from 'hooks/auth/useAuthRTK';

// The literal keys of backend/internal/core/llm/module.go Permissions().
// Every mutation is gated on the permission of ITS resource: managing a
// model does not grant deciding who may use it (spec RBAC). Access
// (granted/everyone) and the grant list are both set on the grants route,
// so both need llm.grants.admin. The usage report has its own read
// permission; the budget is read with llm.admin.read and written with
// llm.budgets.admin. Credential test and model discovery are credential
// operations (llm.credentials.admin); the prompt test is a model one
// (llm.models.admin).
export const LLM_PERMISSIONS = {
  read: 'llm.admin.read',
  credentials: 'llm.credentials.admin',
  models: 'llm.models.admin',
  grants: 'llm.grants.admin',
  usage: 'llm.usage.read',
  budgets: 'llm.budgets.admin',
  // Force-unlink any member's linked ChatGPT account (system permission).
  accounts: 'llm.accounts.admin',
  // Link, select and unlink your own ChatGPT accounts (org permission):
  // gates the AI tab on /user/security.
  accountsSelf: 'llm.accounts.self',
  // The caller's own usable models and month's usage (org permissions).
  modelsSelf: 'llm.models.self',
  usageSelf: 'llm.usage.self'
} as const;

export const useLlmPermissions = () => {
  const { hasPermission } = useAuth();
  const P = LLM_PERMISSIONS;
  return {
    canRead: hasPermission(P.read),
    canManageCredentials: hasPermission(P.credentials),
    canManageModels: hasPermission(P.models),
    canManageGrants: hasPermission(P.grants),
    canReadUsage: hasPermission(P.usage),
    canManageBudget: hasPermission(P.budgets),
    canManageAccounts: hasPermission(P.accounts),
    canLinkOwnAccounts: hasPermission(P.accountsSelf),
    canListOwnModels: hasPermission(P.modelsSelf),
    canReadOwnUsage: hasPermission(P.usageSelf)
  };
};
