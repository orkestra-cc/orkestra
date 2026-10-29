import { useAuth } from 'hooks/auth/useAuthRTK';

// The literal keys of backend/internal/core/retrieval/module.go Permissions().
// The corpus report and diagnostics are installation-wide (system); the
// tenant indexing view and the search log are the active organization's.
export const RETRIEVAL_PERMISSIONS = {
  corpusRead: 'retrieval.corpus.read',
  corpusAdmin: 'retrieval.corpus.admin',
  tenantRead: 'retrieval.tenant.read',
  searchLog: 'retrieval.searchlog.admin'
} as const;

export const useRetrievalPermissions = () => {
  const { hasPermission } = useAuth();
  const P = RETRIEVAL_PERMISSIONS;
  return {
    canReadCorpora: hasPermission(P.corpusRead),
    canAdminCorpora: hasPermission(P.corpusAdmin),
    canReadTenant: hasPermission(P.tenantRead),
    canReadSearchLog: hasPermission(P.searchLog)
  };
};
