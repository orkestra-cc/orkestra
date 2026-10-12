import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Consumer-branching test: the three sources the hook combines are mocked,
// the same way ProtectedRoute's test mocks useAuth.
const modulesQuery = vi.fn();
const navigation = vi.fn();
const hasPermission = vi.fn((_permission: string) => true);

vi.mock('store/api/moduleApi', () => ({
  useGetModulesQuery: () => modulesQuery()
}));
vi.mock('hooks/useRoleBasedNavigation', () => ({
  useRoleBasedNavigation: () => navigation()
}));
vi.mock('hooks/auth/useAuthRTK', () => ({
  useAuth: () => ({ hasPermission })
}));
vi.mock('store/hooks', () => ({ useAppSelector: () => true }));
vi.mock('./index', () => {
  // Defined inside the factory: vi.mock is hoisted above every declaration.
  const Dummy = () => null;
  return {
    moduleCatalog: {
      widgets: {
        name: 'widgets',
        routes: () => [],
        contributions: {
          'reports.detail.tabs': [
            {
              key: 'widgets.summary',
              labelKey: 'tabs.summary',
              component: Dummy
            },
            {
              key: 'widgets.secret',
              labelKey: 'tabs.secret',
              permission: 'widgets.secret.read',
              component: Dummy
            }
          ]
        }
      }
    }
  };
});

import { useSlotContributions } from './useSlotContributions';

const navWith = (moduleName?: string) => ({
  filteredNavigation: moduleName
    ? [{ label: 'Tools', children: [{ name: 'W', moduleName }] }]
    : [],
  realms: [],
  isLoading: false,
  isError: false
});

const running = [{ moduleName: 'widgets', enabled: true, status: 'running' }];

const keysFor = () =>
  renderHook(() =>
    useSlotContributions('reports.detail.tabs')
  ).result.current.map(c => c.key);

beforeEach(() => {
  hasPermission.mockImplementation(() => true);
});

describe('useSlotContributions', () => {
  it('admin: uses the enabled-and-running module list', () => {
    modulesQuery.mockReturnValue({ data: running, isError: false });
    navigation.mockReturnValue(navWith());
    expect(keysFor()).toEqual(['widgets.secret', 'widgets.summary']);
  });

  it('admin: a disabled module contributes nothing even if navigation lists it', () => {
    modulesQuery.mockReturnValue({
      data: [{ moduleName: 'widgets', enabled: false, status: 'disabled' }],
      isError: false
    });
    navigation.mockReturnValue(navWith('widgets'));
    expect(keysFor()).toEqual([]);
  });

  it('non-admin (403): falls back to navigation visibility', () => {
    modulesQuery.mockReturnValue({ data: undefined, isError: true });
    navigation.mockReturnValue(navWith('widgets'));
    expect(keysFor()).toEqual(['widgets.secret', 'widgets.summary']);
    navigation.mockReturnValue(navWith());
    expect(keysFor()).toEqual([]);
  });

  it('renders nothing while modules are unknown', () => {
    modulesQuery.mockReturnValue({ data: undefined, isError: false });
    navigation.mockReturnValue(navWith('widgets'));
    expect(keysFor()).toEqual([]);
  });

  it('hides a contribution whose permission the caller lacks', () => {
    modulesQuery.mockReturnValue({ data: running, isError: false });
    navigation.mockReturnValue(navWith());
    hasPermission.mockImplementation(p => p !== 'widgets.secret.read');
    expect(keysFor()).toEqual(['widgets.summary']);
  });
});
