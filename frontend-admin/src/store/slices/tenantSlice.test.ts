import { describe, it, expect, beforeEach } from 'vitest';
import reducer, {
  setMemberships,
  setCurrentOrg,
  setEffectivePermissions,
  type Membership
} from './tenantSlice';

const memberships: Membership[] = [
  {
    tenantId: 'org-a',
    name: 'Default',
    slug: 'default',
    plan: 'free',
    kind: 'internal',
    roles: ['org_owner'],
    isOwner: true
  },
  {
    tenantId: 'org-b',
    name: 'Second',
    slug: 'second',
    plan: 'free',
    kind: 'internal',
    roles: ['org_owner'],
    isOwner: false
  }
];

// Build a state where org-a is current and its permissions are loaded.
const seededState = () => {
  let state = reducer(undefined, setMemberships(memberships));
  state = reducer(
    state,
    setEffectivePermissions({
      tenantId: state.currentOrgId as string,
      permissions: ['*'],
      systemRole: 'super_admin'
    })
  );
  return state;
};

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

describe('tenantSlice setCurrentOrg', () => {
  it('keeps permissions when re-picking the already-current org', () => {
    const state = seededState();
    expect(state.currentOrgId).toBe('org-a');

    // Re-picking your own workspace (e.g. to exit impersonation) does not
    // change which org the permissions belong to — clearing them here
    // unmounts every hasPermission-gated surface for a network round-trip.
    const next = reducer(state, setCurrentOrg('org-a'));
    expect(next.permissions).toEqual(['*']);
  });

  it('clears permissions when switching to a different org', () => {
    const state = seededState();

    const next = reducer(state, setCurrentOrg('org-b'));
    expect(next.currentOrgId).toBe('org-b');
    expect(next.permissions).toEqual([]);
    expect(next.features).toEqual([]);
  });
});
