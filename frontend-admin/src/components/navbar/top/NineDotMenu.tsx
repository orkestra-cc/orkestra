import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router';
import { Card, Dropdown, Form } from 'react-bootstrap';
import SimpleBar from 'simplebar-react';
import { useTranslation } from 'react-i18next';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  selectCurrentMembership,
  selectImpersonation,
  selectIsImpersonating,
  selectMemberships,
  setCurrentOrg,
  startImpersonation,
  stopImpersonation
} from 'store/slices/tenantSlice';
import { useListAllOrgsAdminQuery } from 'store/api/tenantApi';
import { useAuth } from 'hooks/auth/useAuthRTK';
import { baseApi, TENANT_SCOPED_TAGS } from 'store/api/baseApi';

type TierFilter = 'all' | 'internal' | 'external';

/**
 * NineDotMenu hosts the workspace + operator-impersonation switchers that
 * used to sit as standalone dropdowns in the navbar. The dropdown is hidden
 * when the user has a single membership and cannot impersonate — there's
 * nothing to switch between in that case.
 *
 * Impersonation is gated server-side by system.tenants.admin; the UI gate
 * here is a convenience layer over that permission check.
 */
const NineDotMenu = () => {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const { hasPermission } = useAuth();
  const memberships = useAppSelector(selectMemberships);
  const currentMembership = useAppSelector(selectCurrentMembership);
  const isImpersonating = useAppSelector(selectIsImpersonating);
  const impersonation = useAppSelector(selectImpersonation);

  const canImpersonate =
    hasPermission('system.tenants.admin') &&
    currentMembership?.kind === 'internal';

  const [show, setShow] = useState<boolean>(false);
  const [search, setSearch] = useState('');
  const [tierFilter, setTierFilter] = useState<TierFilter>('all');

  const { data, isLoading } = useListAllOrgsAdminQuery(undefined, {
    skip: !canImpersonate || !show
  });

  const filtered = useMemo(() => {
    const rows = data?.tenants ?? [];
    return rows.filter(tenant => {
      if (tierFilter !== 'all' && tenant.kind !== tierFilter) return false;
      if (!search) return true;
      const q = search.toLowerCase();
      return (
        tenant.name.toLowerCase().includes(q) ||
        tenant.slug.toLowerCase().includes(q) ||
        tenant.id.toLowerCase().includes(q)
      );
    });
  }, [data, search, tierFilter]);

  useEffect(() => {
    const onScroll = () => {
      if (window.innerWidth < 1200) setShow(false);
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  if (memberships.length === 0) return null;
  if (memberships.length <= 1 && !canImpersonate) return null;

  const circleFill = isImpersonating
    ? 'var(--bs-warning)'
    : 'var(--bs-navbar-color, #6C6E71)';

  const onPickMembership = (tenantId: string) => {
    dispatch(setCurrentOrg(tenantId));
    setShow(false);
  };

  const onImpersonate = (tenantId: string, tenantName: string) => {
    dispatch(startImpersonation({ tenantId, tenantName }));
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
    setShow(false);
  };

  const onStopImpersonation = () => {
    dispatch(stopImpersonation());
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
  };

  return (
    <Dropdown
      navbar={true}
      as="li"
      show={show}
      onToggle={next => setShow(next)}
      autoClose="outside"
    >
      <Dropdown.Toggle
        bsPrefix="toggle"
        as={Link}
        to="#!"
        className="nav-link px-2 nine-dots"
        title={
          isImpersonating
            ? t('nav.nineDots.impersonatingTenant', {
                tenant:
                  impersonation.tenantName ??
                  t('nav.nineDots.impersonatingFallback')
              })
            : t('nav.nineDots.workspacesTitle')
        }
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16"
          height="37"
          viewBox="0 0 16 16"
          fill="none"
        >
          <circle cx="2" cy="2" r="2" fill={circleFill}></circle>
          <circle cx="2" cy="8" r="2" fill={circleFill}></circle>
          <circle cx="2" cy="14" r="2" fill={circleFill}></circle>
          <circle cx="8" cy="2" r="2" fill={circleFill}></circle>
          <circle cx="8" cy="8" r="2" fill={circleFill}></circle>
          <circle cx="8" cy="14" r="2" fill={circleFill}></circle>
          <circle cx="14" cy="2" r="2" fill={circleFill}></circle>
          <circle cx="14" cy="8" r="2" fill={circleFill}></circle>
          <circle cx="14" cy="14" r="2" fill={circleFill}></circle>
        </svg>
      </Dropdown.Toggle>

      <Dropdown.Menu
        className="dropdown-menu-card dropdown-menu-end dropdown-caret dropdown-caret-bg"
        show={show}
      >
        <Card className="dropdown-menu-end shadow-none" style={{ width: 340 }}>
          <SimpleBar style={{ maxHeight: 520 }}>
            <Card.Body className="p-0">
              <div className="px-3 pt-3 pb-2">
                <h6 className="mb-2 text-uppercase text-700 fs-11 fw-bold">
                  {t('nav.nineDots.yourWorkspaces')}
                </h6>
                {memberships.map(m => {
                  const active = m.tenantId === currentMembership?.tenantId;
                  return (
                    <button
                      key={m.tenantId}
                      type="button"
                      className={`btn btn-sm w-100 text-start mb-1 ${
                        active
                          ? 'btn-soft-primary'
                          : 'btn-link text-decoration-none'
                      }`}
                      onClick={() => onPickMembership(m.tenantId)}
                    >
                      <div className="d-flex justify-content-between align-items-center">
                        <span className="fw-semibold text-truncate">
                          {m.name}
                        </span>
                        <small className="text-muted ms-2 flex-shrink-0">
                          {m.plan}
                        </small>
                      </div>
                      {m.roles.length > 0 && (
                        <small className="text-muted d-block text-truncate">
                          {m.roles.join(', ')}
                        </small>
                      )}
                    </button>
                  );
                })}
              </div>

              {canImpersonate && (
                <>
                  <hr className="my-0" />
                  <div className="px-3 pt-3 pb-3">
                    <h6 className="mb-2 text-uppercase text-700 fs-11 fw-bold">
                      {t('nav.nineDots.impersonateTenant')}
                    </h6>

                    {isImpersonating && (
                      <div className="alert alert-warning py-2 px-3 mb-2 d-flex justify-content-between align-items-center">
                        <small className="text-truncate">
                          {t('nav.nineDots.impersonating')}{' '}
                          <strong>{impersonation.tenantName}</strong>
                        </small>
                        <button
                          type="button"
                          className="btn btn-sm btn-warning ms-2 flex-shrink-0"
                          onClick={onStopImpersonation}
                        >
                          {t('nav.nineDots.stop')}
                        </button>
                      </div>
                    )}

                    <Form.Control
                      type="search"
                      size="sm"
                      placeholder={t('nav.nineDots.searchPlaceholder')}
                      value={search}
                      onChange={e => setSearch(e.target.value)}
                    />
                    <div className="d-flex gap-1 mt-2 mb-2">
                      {(['all', 'internal', 'external'] as TierFilter[]).map(
                        tier => (
                          <button
                            key={tier}
                            type="button"
                            className={`btn btn-sm ${
                              tierFilter === tier
                                ? 'btn-secondary'
                                : 'btn-outline-secondary'
                            }`}
                            onClick={() => setTierFilter(tier)}
                          >
                            {t(`nav.nineDots.tier.${tier}` as const)}
                          </button>
                        )
                      )}
                    </div>

                    {isLoading && (
                      <p className="text-muted small mb-0">
                        {t('nav.nineDots.loading')}
                      </p>
                    )}
                    {!isLoading && filtered.length === 0 && (
                      <p className="text-muted small mb-0">
                        {t('nav.nineDots.noMatch')}
                      </p>
                    )}
                    <div className="d-flex flex-column">
                      {filtered.slice(0, 200).map(tenant => {
                        const active = impersonation.tenantId === tenant.id;
                        return (
                          <button
                            key={tenant.id}
                            type="button"
                            className={`btn btn-sm text-start mb-1 ${
                              active
                                ? 'btn-soft-warning'
                                : 'btn-link text-decoration-none'
                            }`}
                            onClick={() =>
                              onImpersonate(tenant.id, tenant.name)
                            }
                          >
                            <div className="d-flex justify-content-between align-items-center">
                              <span className="fw-semibold text-truncate">
                                {tenant.name}
                              </span>
                              <small className="text-muted ms-2 flex-shrink-0">
                                {tenant.kind ?? '—'}
                              </small>
                            </div>
                            <small className="text-muted d-block text-truncate">
                              {tenant.slug} · {tenant.plan}
                            </small>
                          </button>
                        );
                      })}
                    </div>
                    {filtered.length > 200 && (
                      <p className="text-muted small mb-0">
                        {t('nav.nineDots.refineSearch', {
                          count: filtered.length
                        })}
                      </p>
                    )}
                  </div>
                </>
              )}
            </Card.Body>
          </SimpleBar>
        </Card>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default NineDotMenu;
