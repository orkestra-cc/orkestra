import { lazy } from 'react';
import { describe, expect, it, vi } from 'vitest';
import type { ModuleManifest } from './types';
import { ADMIN_SECTION_KEY, validAdminSections } from './adminSections';
import { moduleCatalog } from './index';

const component = lazy(async () => ({ default: () => null }));
const manifest = (keys: string[]): ModuleManifest => ({
  name: 'demo',
  routes: () => [],
  adminSections: keys.map(key => ({ key, labelKey: `demo.${key}`, component }))
});

describe('validAdminSections', () => {
  it('keeps well-formed unique keys in order', () => {
    expect(
      validAdminSections(manifest(['tools', 'audit-log', 'x_1'])).map(
        s => s.key
      )
    ).toEqual(['tools', 'audit-log', 'x_1']);
  });

  it('drops invalid and duplicate keys, the first duplicate winning', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const kept = validAdminSections(
      manifest(['tools', 'Tools', '-x', 'a b', '', 'tools', 'ok'])
    );
    expect(kept.map(s => s.key)).toEqual(['tools', 'ok']);
    warn.mockRestore();
  });

  it('returns nothing for a manifest without sections', () => {
    expect(validAdminSections(undefined)).toEqual([]);
    expect(validAdminSections({ name: 'x', routes: () => [] })).toEqual([]);
  });
});

describe('module catalog admin sections', () => {
  it('declares only valid, unique keys in every shipped manifest', () => {
    for (const shipped of Object.values(moduleCatalog)) {
      const keys = (shipped.adminSections ?? []).map(s => s.key);
      expect(new Set(keys).size).toBe(keys.length);
      for (const key of keys) expect(key).toMatch(ADMIN_SECTION_KEY);
    }
  });
});
