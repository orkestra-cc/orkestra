import { describe, expect, it } from 'vitest';
import { collectVisibleModuleNames } from './ModuleGlobalOverlays';

describe('collectVisibleModuleNames', () => {
  it('collects module owners from both navigation shapes recursively', () => {
    const modules = collectVisibleModuleNames(
      [
        {
          label: 'Tools',
          children: [
            {
              name: 'Parent',
              children: [{ name: 'Widget list', moduleName: 'widgets' }]
            }
          ]
        }
      ],
      [
        {
          key: 'business',
          label: 'Business',
          sections: [
            {
              label: 'Reports',
              children: [{ name: 'Monthly', moduleName: 'reports' }]
            }
          ]
        }
      ]
    );

    expect([...modules].sort()).toEqual(['reports', 'widgets']);
  });

  it('does not infer a module from an unowned navigation item', () => {
    const modules = collectVisibleModuleNames(
      [{ label: 'Core', children: [{ name: 'Home', to: '/' }] }],
      []
    );

    expect([...modules]).toEqual([]);
  });
});
