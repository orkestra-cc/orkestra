// Owner-scope switcher used by every dashboard page (subscriptions,
// transactions, payment methods). Renders nothing when the caller has
// no owned tenants — single-owner clients (the default Tier-2 shape)
// see no UI clutter and stay in implicit "personal" mode (the "all"
// scope still fans out, but it's a no-op for them since their only
// owner is themselves).
import { useTranslation } from 'react-i18next';

import { shortTenantLabel, type OwnerScope } from '@/auth/ownerScope';
import type { JwtMembership } from '@/auth/memberships';

const ALL_VALUE = '__all__';
const PERSONAL_VALUE = '__personal__';

interface OwnerScopeSwitcherProps {
  scope: OwnerScope;
  setScope: (next: OwnerScope) => void;
  ownedTenants: JwtMembership[];
  hasTenants: boolean;
}

export function OwnerScopeSwitcher({
  scope,
  setScope,
  ownedTenants,
  hasTenants,
}: OwnerScopeSwitcherProps) {
  const { t } = useTranslation();

  if (!hasTenants) return null;

  const value =
    scope.kind === 'all' ? ALL_VALUE : scope.kind === 'user' ? PERSONAL_VALUE : scope.uuid;

  function handleChange(next: string) {
    if (next === ALL_VALUE) {
      setScope({ kind: 'all' });
      return;
    }
    if (next === PERSONAL_VALUE) {
      setScope({ kind: 'user' });
      return;
    }
    setScope({ kind: 'tenant', uuid: next });
  }

  return (
    <div className="flex items-center gap-2">
      <label htmlFor="owner-scope" className="text-sm font-medium text-slate-600">
        {t('ownerScope.label')}
      </label>
      <select
        id="owner-scope"
        value={value}
        onChange={(e) => handleChange(e.target.value)}
        className="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      >
        <option value={ALL_VALUE}>{t('ownerScope.all')}</option>
        <option value={PERSONAL_VALUE}>{t('ownerScope.personal')}</option>
        {ownedTenants.map((m) => (
          <option key={m.tenantUuid} value={m.tenantUuid}>
            {t('ownerScope.tenantOption', { id: shortTenantLabel(m.tenantUuid) })}
          </option>
        ))}
      </select>
    </div>
  );
}
