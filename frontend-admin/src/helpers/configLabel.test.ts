import { describe, it, expect, beforeAll } from 'vitest';
import i18n from '../i18n';
import type { ConfigField } from 'store/api/moduleApi';
import { translateConfigField, translateConfigGroup } from './configLabel';

const field: ConfigField = {
  key: 'passwordMinLength',
  label: 'Minimum length',
  description: 'Shortest accepted password.',
  type: 'int',
  required: false,
  default: '',
  envVar: ''
};

describe('translateConfigField', () => {
  beforeAll(async () => {
    await i18n.changeLanguage('en');
    // A fork addon's own namespace (ADR-0007).
    i18n.addResourceBundle(
      'en',
      'billing',
      { config: { fields: { passwordMinLength: { label: 'From addon ns' } } } },
      true,
      true
    );
    // The core bundle.
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          auth: {
            fields: { passwordMinLength: { label: 'From core bundle' } }
          }
        }
      },
      true,
      true
    );
  });

  it("prefers the module's own namespace when it has the key", () => {
    expect(translateConfigField(i18n.t, 'billing', field, 'label')).toBe(
      'From addon ns'
    );
  });

  it('falls back to the core bundle for a core module', () => {
    expect(translateConfigField(i18n.t, 'auth', field, 'label')).toBe(
      'From core bundle'
    );
  });

  it('falls back to the literal from the backend when neither has the key', () => {
    // An un-migrated fork addon must keep showing English, never a raw key.
    expect(translateConfigField(i18n.t, 'unknownmod', field, 'label')).toBe(
      'Minimum length'
    );
    expect(translateConfigField(i18n.t, 'unknownmod', field, 'desc')).toBe(
      'Shortest accepted password.'
    );
  });

  it('falls through to the literal when the addon namespace has an explicit empty string', () => {
    // i18next's `returnEmptyString` default (true) resolves a key present
    // as "" to "" rather than consulting `defaultValue` — a blank
    // translation must not blank the label.
    i18n.addResourceBundle(
      'en',
      'blankaddon',
      { config: { fields: { passwordMinLength: { label: '' } } } },
      true,
      true
    );
    expect(translateConfigField(i18n.t, 'blankaddon', field, 'label')).toBe(
      'Minimum length'
    );
  });

  it('falls through to the literal when the core bundle has an explicit empty string', () => {
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          blankcore: {
            fields: { passwordMinLength: { label: '' } }
          }
        }
      },
      true,
      true
    );
    expect(translateConfigField(i18n.t, 'blankcore', field, 'label')).toBe(
      'Minimum length'
    );
  });
});

describe('translateConfigGroup', () => {
  it('falls back to the declared label', () => {
    expect(
      translateConfigGroup(i18n.t, 'unknownmod', {
        key: 'oauth',
        label: 'OAuth Providers'
      })
    ).toBe('OAuth Providers');
  });

  it("prefers the module's own namespace when it has the key", () => {
    i18n.addResourceBundle(
      'en',
      'groupaddon',
      { config: { groups: { oauth: { label: 'From addon ns (group)' } } } },
      true,
      true
    );
    expect(
      translateConfigGroup(i18n.t, 'groupaddon', {
        key: 'oauth',
        label: 'OAuth Providers'
      })
    ).toBe('From addon ns (group)');
  });

  it('falls back to the core bundle for a core module', () => {
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          groupcore: {
            groups: { oauth: { label: 'From core bundle (group)' } }
          }
        }
      },
      true,
      true
    );
    expect(
      translateConfigGroup(i18n.t, 'groupcore', {
        key: 'oauth',
        label: 'OAuth Providers'
      })
    ).toBe('From core bundle (group)');
  });

  it('resolves a description via the desc part, falling back to the declared description', () => {
    expect(
      translateConfigGroup(
        i18n.t,
        'unknownmod',
        {
          key: 'oauth',
          label: 'OAuth Providers',
          description: 'Configure OAuth providers.'
        },
        'desc'
      )
    ).toBe('Configure OAuth providers.');
  });

  it("prefers the module's own namespace for a description", () => {
    i18n.addResourceBundle(
      'en',
      'groupaddon',
      {
        config: {
          groups: { oauth: { desc: 'From addon ns (group desc)' } }
        }
      },
      true,
      true
    );
    expect(
      translateConfigGroup(
        i18n.t,
        'groupaddon',
        {
          key: 'oauth',
          label: 'OAuth Providers',
          description: 'Configure OAuth providers.'
        },
        'desc'
      )
    ).toBe('From addon ns (group desc)');
  });

  // `src/i18n.ts` sets no `keySeparator`, so i18next falls back to its
  // default `.` and splits every key into a path. auth's four OAuth
  // sub-group keys contain a dot (`oauth.google`, `oauth.apple`, ...), so
  // `t('moduleConfig.auth.groups.oauth.google.label')` must walk
  // groups -> oauth -> google -> label through NESTED objects. This test
  // proves that shape actually resolves — a wrong assumption here would
  // fail *silently*, falling back to the backend's English literal, which
  // is indistinguishable from "key not written yet".
  it('resolves a nested group key (dotted) from a NESTED bundle — parent and child both resolve', () => {
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          nestedcore: {
            groups: {
              oauth: {
                label: 'OAuth Providers (parent, from core bundle)',
                google: { label: 'Google (child, from core bundle)' }
              }
            }
          }
        }
      },
      true,
      true
    );

    expect(
      translateConfigGroup(i18n.t, 'nestedcore', {
        key: 'oauth',
        label: 'OAuth Providers'
      })
    ).toBe('OAuth Providers (parent, from core bundle)');

    expect(
      translateConfigGroup(i18n.t, 'nestedcore', {
        key: 'oauth.google',
        label: 'Google'
      })
    ).toBe('Google (child, from core bundle)');
  });

  // A literal JS property named "oauth.google" one level deep also
  // resolves — verified here rather than assumed. i18next's `getPath`
  // walks the split segments and, on a miss, re-joins trailing segments
  // with `.` to retry as a single literal key (`getLastOfPath` in
  // `node_modules/i18next/dist/cjs/i18next.js`), so a flat dotted property
  // is not the silent-failure trap it might appear to be. This does NOT
  // change what we author: the brief's nested shape is still correct
  // (proven above) and is what we actually ship, since `oauth`'s own
  // `label`/`desc` has to live somewhere and nesting its four children
  // under it is the natural place — this test just records that the flat
  // form isn't a load-bearing hazard we got lucky avoiding.
  it('documents that i18next ALSO resolves a flat dotted-key property (not the trap it looks like)', () => {
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        moduleConfig: {
          flatcore: {
            groups: {
              'oauth.google': {
                label: 'Google (flat property, one level deep)'
              }
            }
          }
        }
      },
      true,
      true
    );

    expect(
      translateConfigGroup(i18n.t, 'flatcore', {
        key: 'oauth.google',
        label: 'Google (literal fallback, should not appear)'
      })
    ).toBe('Google (flat property, one level deep)');
  });
});
