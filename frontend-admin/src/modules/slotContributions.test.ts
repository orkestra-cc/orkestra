import { lazy } from 'react';
import { describe, expect, it } from 'vitest';
import type { ModuleManifest } from './types';
import {
  activeModuleNames,
  collectSlotContributions
} from './slotContributions';

const Dummy = lazy(async () => ({ default: () => null }));

const manifest = (
  name: string,
  contributions: ModuleManifest['contributions']
): ModuleManifest => ({
  name,
  routes: () => [],
  contributions
});

const catalog: Record<string, ModuleManifest> = {
  widgets: manifest('widgets', {
    'reports.detail.tabs': [
      { key: 'widgets.summary', labelKey: 'tabs.summary', component: Dummy },
      {
        key: 'widgets.secret',
        labelKey: 'tabs.secret',
        permission: 'widgets.secret.read',
        component: Dummy
      },
      // Not namespaced by its module: dropped.
      { key: 'summary', labelKey: 'tabs.bad', component: Dummy }
    ]
  }),
  alpha: manifest('alpha', {
    'reports.detail.tabs': [
      { key: 'alpha.extra', labelKey: 'tabs.extra', component: Dummy }
    ]
  }),
  quiet: manifest('quiet', undefined)
};

const all = () => true;

describe('collectSlotContributions', () => {
  it('collects namespaced contributions of active modules, sorted by key', () => {
    const got = collectSlotContributions(
      catalog,
      'reports.detail.tabs',
      new Set(['widgets', 'alpha', 'quiet']),
      all
    );
    expect(got.map(c => [c.key, c.moduleName])).toEqual([
      ['alpha.extra', 'alpha'],
      ['widgets.secret', 'widgets'],
      ['widgets.summary', 'widgets']
    ]);
  });

  it('skips inactive modules, other slots and unknown state', () => {
    expect(
      collectSlotContributions(
        catalog,
        'reports.detail.tabs',
        new Set(['alpha']),
        all
      ).map(c => c.key)
    ).toEqual(['alpha.extra']);
    expect(
      collectSlotContributions(
        catalog,
        'other.slot',
        new Set(['widgets', 'alpha']),
        all
      )
    ).toEqual([]);
    expect(
      collectSlotContributions(catalog, 'reports.detail.tabs', null, all)
    ).toEqual([]);
  });

  it('filters by permission', () => {
    const got = collectSlotContributions(
      catalog,
      'reports.detail.tabs',
      new Set(['widgets']),
      p => p !== 'widgets.secret.read'
    );
    expect(got.map(c => c.key)).toEqual(['widgets.summary']);
  });
});

describe('activeModuleNames', () => {
  const admin = [
    { moduleName: 'widgets', enabled: true, status: 'running' },
    { moduleName: 'alpha', enabled: true, status: 'failed' },
    { moduleName: 'quiet', enabled: false, status: 'disabled' }
  ];

  it('uses the admin module list when available (enabled and running)', () => {
    expect([
      ...(activeModuleNames(admin, false, new Set(['quiet'])) ?? [])
    ]).toEqual(['widgets']);
  });

  it('falls back to navigation-visible modules when the admin list is refused', () => {
    expect([
      ...(activeModuleNames(undefined, true, new Set(['alpha'])) ?? [])
    ]).toEqual(['alpha']);
    expect(activeModuleNames(undefined, true, null)).toBeNull();
  });

  it('is unknown while the admin list is loading', () => {
    expect(activeModuleNames(undefined, false, new Set(['alpha']))).toBeNull();
  });
});
