import { useMemo, useState } from 'react';
import { Dropdown, Form } from 'react-bootstrap';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  selectCurrentMembership,
  selectImpersonation,
  selectIsImpersonating,
  startImpersonation,
  stopImpersonation
} from 'store/slices/tenantSlice';
import { useListAllOrgsAdminQuery } from 'store/api/tenantApi';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { baseApi, TENANT_SCOPED_TAGS } from 'store/api/baseApi';

type TierFilter = 'all' | 'internal' | 'external';

/**
 * AdminTenantSwitcher lets an internal operator holding system.tenants.admin
 * impersonate any tenant platform-wide. Dispatches startImpersonation into
 * the tenant slice and resets the RTK Query cache so per-tenant data is
 * re-fetched against the target. The backend gate is in
 * shared/middleware/auth.go — non-admin callers sending the same X-Tenant-ID
 * still get 403, so this UI is a convenience layer over a server-side
 * permission check, not the authorization itself.
 */
export default function AdminTenantSwitcher() {
  const dispatch = useAppDispatch();
  const { hasPermission } = useAuth();
  const currentMembership = useAppSelector(selectCurrentMembership);
  const isImpersonating = useAppSelector(selectIsImpersonating);
  const impersonation = useAppSelector(selectImpersonation);

  const canImpersonate =
    hasPermission('system.tenants.admin') &&
    currentMembership?.kind === 'internal';

  const [search, setSearch] = useState('');
  const [tierFilter, setTierFilter] = useState<TierFilter>('all');

  const { data, isLoading } = useListAllOrgsAdminQuery(undefined, {
    skip: !canImpersonate
  });

  const filtered = useMemo(() => {
    const rows = data?.tenants ?? [];
    return rows.filter((t) => {
      if (tierFilter !== 'all' && t.kind !== tierFilter) return false;
      if (!search) return true;
      const q = search.toLowerCase();
      return (
        t.name.toLowerCase().includes(q) ||
        t.slug.toLowerCase().includes(q) ||
        t.id.toLowerCase().includes(q)
      );
    });
  }, [data, search, tierFilter]);

  if (!canImpersonate) {
    return null;
  }

  const onSelect = (tenantId: string, tenantName: string) => {
    dispatch(startImpersonation({ tenantId, tenantName }));
    // Invalidate every tenant-scoped tag so the next render refetches
    // against the impersonated tenant. resetApiState() would do the same
    // but also nukes the session cache, producing a render where
    // ProtectedRoute sees !isAuthenticated and bounces to /login.
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
  };

  const onStop = () => {
    dispatch(stopImpersonation());
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
  };

  const toggleLabel = isImpersonating
    ? `Impersonating: ${impersonation.tenantName ?? 'tenant'}`
    : 'Impersonate tenant';

  return (
    <Dropdown align="end" className="me-2" autoClose="outside">
      <Dropdown.Toggle
        variant={isImpersonating ? 'warning' : 'outline-warning'}
        size="sm"
        id="admin-tenant-switcher"
      >
        {toggleLabel}
      </Dropdown.Toggle>
      <Dropdown.Menu style={{ minWidth: 320, maxHeight: 480, overflowY: 'auto' }}>
        <Dropdown.Header>Operator impersonation</Dropdown.Header>
        {isImpersonating && (
          <>
            <Dropdown.Item
              onClick={onStop}
              className="text-danger fw-semibold"
            >
              Stop impersonating
            </Dropdown.Item>
            <Dropdown.Divider />
          </>
        )}
        <div className="px-3 pb-2">
          <Form.Control
            type="search"
            size="sm"
            placeholder="Search by name, slug, or id"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            autoFocus
          />
          <div className="d-flex gap-1 mt-2">
            {(['all', 'internal', 'external'] as TierFilter[]).map((t) => (
              <button
                key={t}
                type="button"
                className={`btn btn-sm ${
                  tierFilter === t ? 'btn-secondary' : 'btn-outline-secondary'
                }`}
                onClick={() => setTierFilter(t)}
              >
                {t}
              </button>
            ))}
          </div>
        </div>
        <Dropdown.Divider />
        {isLoading && <Dropdown.ItemText>Loading…</Dropdown.ItemText>}
        {!isLoading && filtered.length === 0 && (
          <Dropdown.ItemText className="text-muted small">
            No tenants match.
          </Dropdown.ItemText>
        )}
        {filtered.slice(0, 200).map((t) => (
          <Dropdown.Item
            key={t.id}
            active={impersonation.tenantId === t.id}
            onClick={() => onSelect(t.id, t.name)}
          >
            <div className="d-flex justify-content-between align-items-center">
              <span className="fw-semibold">{t.name}</span>
              <small className="text-muted ms-3">{t.kind ?? '—'}</small>
            </div>
            <small className="text-muted d-block">
              {t.slug} · {t.plan}
            </small>
          </Dropdown.Item>
        ))}
        {filtered.length > 200 && (
          <Dropdown.ItemText className="text-muted small">
            Showing first 200 of {filtered.length} — refine the search.
          </Dropdown.ItemText>
        )}
      </Dropdown.Menu>
    </Dropdown>
  );
}
