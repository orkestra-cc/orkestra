import { describe, it, expect } from 'vitest';
import type { ConfigField, ConfigGroup } from 'store/api/moduleApi';
import {
  buildGroupTree,
  isFieldVisible,
  visibleFields,
  configCompleteness,
  flattenTree
} from './configModel';

const field = (over: Partial<ConfigField> & { key: string }): ConfigField => ({
  label: over.key,
  description: '',
  type: 'string',
  required: false,
  default: '',
  envVar: '',
  ...over
});

describe('buildGroupTree — legacy modules (no declared groups)', () => {
  // Every module in the tree today is in this state: `group` holds a display
  // label and no `configGroups` is returned. The tree must reproduce exactly
  // what the old bucketByGroup produced, or the tabs visibly change.
  it('buckets by declaration order and trails ungrouped fields under General', () => {
    const schema = [
      field({ key: 'a', group: 'Google' }),
      field({ key: 'b' }),
      field({ key: 'c', group: 'Google' }),
      field({ key: 'd', group: 'Apple' })
    ];
    const tree = buildGroupTree(schema, undefined);
    expect(tree.map(n => n.key)).toEqual(['Google', 'Apple', 'General']);
    expect(tree.map(n => n.label)).toEqual(['Google', 'Apple', 'General']);
    expect(tree[0].fieldKeys).toEqual(['a', 'c']);
    expect(tree[2].fieldKeys).toEqual(['b']);
    expect(tree.every(n => n.children.length === 0)).toBe(true);
  });

  it('returns an empty tree for an empty schema', () => {
    expect(buildGroupTree([], undefined)).toEqual([]);
  });
});

describe('buildGroupTree — declared groups', () => {
  const groups: ConfigGroup[] = [
    { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
    { key: 'oauth', label: 'OAuth Providers', order: 1 },
    { key: 'password', label: 'Password Policy', order: 3 }
  ];
  const schema = [
    field({ key: 'googleId', group: 'oauth.google' }),
    field({ key: 'googleOn', group: 'oauth' }),
    field({ key: 'minLen', group: 'password' })
  ];

  it('nests children under their parent and sorts by order', () => {
    const tree = buildGroupTree(schema, groups);
    expect(tree.map(n => n.key)).toEqual(['oauth', 'password']);
    expect(tree[0].children.map(n => n.key)).toEqual(['oauth.google']);
    expect(tree[0].fieldKeys).toEqual(['googleOn']);
    expect(tree[0].children[0].fieldKeys).toEqual(['googleId']);
  });

  it('keeps an ungrouped field reachable instead of dropping it', () => {
    // The backend validator rejects this, but the UI must never make a field
    // unreachable if a non-compliant module slips through.
    const tree = buildGroupTree([...schema, field({ key: 'orphan' })], groups);
    const general = tree.find(n => n.key === 'General');
    expect(general?.fieldKeys).toEqual(['orphan']);
  });

  it('ignores a child whose parent was never declared', () => {
    const tree = buildGroupTree(
      [field({ key: 'x', group: 'ghostchild' })],
      [{ key: 'ghostchild', label: 'Ghost child', parent: 'nowhere' }]
    );
    // Promoted to top level rather than lost.
    expect(tree.map(n => n.key)).toEqual(['ghostchild']);
    expect(tree[0].fieldKeys).toEqual(['x']);
  });
});

describe('isFieldVisible', () => {
  const schema = [
    field({ key: 'googleOn', type: 'bool', default: 'false' }),
    field({
      key: 'provider',
      type: 'enum',
      default: 'noop',
      options: ['noop', 'smtp']
    }),
    field({ key: 'clientId', dependsOn: [{ key: 'googleOn', in: ['true'] }] }),
    field({ key: 'smtpHost', dependsOn: [{ key: 'provider', in: ['smtp'] }] })
  ];

  it('is visible when the field declares no condition', () => {
    expect(isFieldVisible(schema[0], {}, schema)).toBe(true);
  });

  it('reads a bool target through bool semantics, not string equality', () => {
    // The backend seeds the raw env string, so "1" is what an operator who set
    // GOOGLE_ENABLED=1 actually has stored. Exact string match would hide the
    // field forever.
    const f = schema[2];
    expect(isFieldVisible(f, { googleOn: 'true' }, schema)).toBe(true);
    expect(isFieldVisible(f, { googleOn: '1' }, schema)).toBe(true);
    expect(isFieldVisible(f, { googleOn: 'YES' }, schema)).toBe(true);
    expect(isFieldVisible(f, { googleOn: 'false' }, schema)).toBe(false);
    expect(isFieldVisible(f, { googleOn: '' }, schema)).toBe(false);
  });

  it("falls back to the target's default when no value is stored", () => {
    const s = [
      field({ key: 'on', type: 'bool', default: 'true' }),
      field({ key: 'dep', dependsOn: [{ key: 'on', in: ['true'] }] })
    ];
    expect(isFieldVisible(s[1], {}, s)).toBe(true);
  });

  it('matches non-bool targets case-insensitively and trimmed', () => {
    const f = schema[3];
    expect(isFieldVisible(f, { provider: 'smtp' }, schema)).toBe(true);
    expect(isFieldVisible(f, { provider: ' SMTP ' }, schema)).toBe(true);
    expect(isFieldVisible(f, { provider: 'noop' }, schema)).toBe(false);
  });

  it('ANDs across conditions and ORs within one condition', () => {
    const s = [
      field({ key: 'a', type: 'bool', default: 'false' }),
      field({ key: 'b', type: 'enum', default: '', options: ['x', 'y', 'z'] }),
      field({
        key: 'dep',
        dependsOn: [
          { key: 'a', in: ['true'] },
          { key: 'b', in: ['x', 'y'] }
        ]
      })
    ];
    expect(isFieldVisible(s[2], { a: 'true', b: 'y' }, s)).toBe(true);
    expect(isFieldVisible(s[2], { a: 'true', b: 'z' }, s)).toBe(false);
    expect(isFieldVisible(s[2], { a: 'false', b: 'x' }, s)).toBe(false);
  });

  it('treats a condition on an unknown field as unsatisfied', () => {
    const f = field({ key: 'dep', dependsOn: [{ key: 'nope', in: ['true'] }] });
    expect(isFieldVisible(f, {}, [f])).toBe(false);
  });
});

describe('configCompleteness', () => {
  it('counts only required fields that are visible', () => {
    // Without this, `auth` reports a fraction that counts credentials belonging
    // to switched-off OAuth providers — a number that is simply false.
    const schema = [
      field({ key: 'googleOn', type: 'bool', default: 'false' }),
      field({
        key: 'clientId',
        required: true,
        dependsOn: [{ key: 'googleOn', in: ['true'] }]
      }),
      field({ key: 'always', required: true })
    ];
    expect(configCompleteness(schema, { always: 'set' }, {})).toEqual({
      filled: 1,
      total: 1
    });
    expect(
      configCompleteness(schema, { googleOn: 'true', always: 'set' }, {})
    ).toEqual({ filled: 1, total: 2 });
  });

  it('counts a stored secret as filled', () => {
    const schema = [field({ key: 's', type: 'secret', required: true })];
    expect(configCompleteness(schema, {}, { s: true })).toEqual({
      filled: 1,
      total: 1
    });
    expect(configCompleteness(schema, {}, { s: false })).toEqual({
      filled: 0,
      total: 1
    });
  });

  it('returns zeroes for a null schema', () => {
    expect(configCompleteness(null, {}, {})).toEqual({ filled: 0, total: 0 });
  });
});

describe('flattenTree', () => {
  it('returns parents before children, depth-first', () => {
    const schema = [
      field({ key: 'a', group: 'oauth' }),
      field({ key: 'b', group: 'oauth.google' }),
      field({ key: 'c', group: 'password' })
    ];
    const groups = [
      { key: 'oauth', label: 'OAuth', order: 1 },
      { key: 'oauth.google', label: 'Google', parent: 'oauth', order: 2 },
      { key: 'password', label: 'Password', order: 3 }
    ];
    expect(flattenTree(buildGroupTree(schema, groups)).map(n => n.key)).toEqual(
      ['oauth', 'oauth.google', 'password']
    );
  });
});

describe('visibleFields', () => {
  it('drops hidden fields and preserves schema order', () => {
    const schema = [
      field({ key: 'on', type: 'bool', default: 'false' }),
      field({ key: 'hidden', dependsOn: [{ key: 'on', in: ['true'] }] }),
      field({ key: 'shown' })
    ];
    expect(visibleFields(schema, {}).map(f => f.key)).toEqual(['on', 'shown']);
    expect(visibleFields(schema, { on: 'true' }).map(f => f.key)).toEqual([
      'on',
      'hidden',
      'shown'
    ]);
  });
});
