import { describe, it, expect } from 'vitest';
import type { ConfigField, ConfigGroup } from 'store/api/moduleApi';
import {
  buildGroupTree,
  isFieldVisible,
  visibleFields,
  configCompleteness,
  flattenTree,
  hasCardRail,
  hasPageRail
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

  it('records the declared parent on a child, and nothing on a root', () => {
    // `ModuleConfigPanel`'s all-hidden-leaf empty state needs to name its
    // parent and link back to it without being handed the whole tree, so the
    // node carries the parent's key + literal label — exactly
    // `translateConfigGroup`'s input shape.
    const tree = buildGroupTree(schema, groups);
    expect(tree[0].parent).toBeUndefined();
    expect(tree[0].children[0].parent).toEqual({
      key: 'oauth',
      label: 'OAuth Providers'
    });
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
    // Promoted to top level rather than lost — and with no `parent`, so the
    // empty state can't offer a link to a group that does not exist.
    expect(tree.map(n => n.key)).toEqual(['ghostchild']);
    expect(tree[0].fieldKeys).toEqual(['x']);
    expect(tree[0].parent).toBeUndefined();
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

  it("ORs the conditions when dependsOnMatch is 'any'", () => {
    // Each OAuth provider has one toggle per audience surface, and its
    // credentials are needed as soon as either is on.
    const s = [
      field({ key: 'admin', type: 'bool', default: 'false' }),
      field({ key: 'client', type: 'bool', default: 'false' }),
      field({
        key: 'cred',
        dependsOnMatch: 'any',
        dependsOn: [
          { key: 'admin', in: ['true'] },
          { key: 'client', in: ['true'] }
        ]
      })
    ];
    expect(isFieldVisible(s[2], {}, s)).toBe(false);
    expect(isFieldVisible(s[2], { admin: 'true' }, s)).toBe(true);
    expect(isFieldVisible(s[2], { client: 'true' }, s)).toBe(true);
    expect(isFieldVisible(s[2], { admin: 'true', client: 'true' }, s)).toBe(
      true
    );
  });

  it("keeps AND semantics when dependsOnMatch is absent or 'all'", () => {
    const build = (match?: 'all' | 'any') => [
      field({ key: 'a', type: 'bool', default: 'false' }),
      field({ key: 'b', type: 'bool', default: 'false' }),
      field({
        key: 'dep',
        dependsOnMatch: match,
        dependsOn: [
          { key: 'a', in: ['true'] },
          { key: 'b', in: ['true'] }
        ]
      })
    ];
    for (const match of [undefined, 'all' as const]) {
      const s = build(match);
      expect(isFieldVisible(s[2], { a: 'true' }, s)).toBe(false);
      expect(isFieldVisible(s[2], { a: 'true', b: 'true' }, s)).toBe(true);
    }
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

describe('hasCardRail vs hasPageRail', () => {
  // The reachable disagreement between the two formulas: a module that
  // *declares* configGroups but whose tree collapses to a single top-level
  // root (it can still have nested children — that's the whole point of
  // opting in). The card gets a rail for it; the page does not, and
  // degrades to the stacked page with that card inside it.
  it('declared groups collapsing to one top-level root: card yes, page no', () => {
    const schema = [
      field({ key: 'a', group: 'only' }),
      field({ key: 'b', group: 'only.child' })
    ];
    const groups: ConfigGroup[] = [
      { key: 'only', label: 'Only', order: 1 },
      { key: 'only.child', label: 'Child', parent: 'only', order: 2 }
    ];
    const tree = buildGroupTree(schema, groups);
    expect(tree.map(n => n.key)).toEqual(['only']);
    expect(hasCardRail(tree, groups)).toBe(true);
    expect(hasPageRail(tree, groups)).toBe(false);
  });

  it('legacy modules (no declared groups) never get the page rail, even with several buckets', () => {
    // Every module served today is in this state. hasCardRail's legacy
    // heuristic (≥2 distinct field.group labels) still applies to the card,
    // but hasPageRail requires an explicit opt-in via declared configGroups.
    const schema = [
      field({ key: 'a', group: 'Google' }),
      field({ key: 'b', group: 'Apple' }),
      field({ key: 'c', group: 'GitHub' })
    ];
    const tree = buildGroupTree(schema, undefined);
    expect(tree.length).toBeGreaterThanOrEqual(2);
    expect(hasCardRail(tree, undefined)).toBe(true);
    expect(hasPageRail(tree, undefined)).toBe(false);
  });

  it('both agree once ≥2 top-level groups are declared', () => {
    const schema = [
      field({ key: 'a', group: 'g1' }),
      field({ key: 'b', group: 'g2' })
    ];
    const groups: ConfigGroup[] = [
      { key: 'g1', label: 'Group One', order: 1 },
      { key: 'g2', label: 'Group Two', order: 2 }
    ];
    const tree = buildGroupTree(schema, groups);
    expect(hasCardRail(tree, groups)).toBe(true);
    expect(hasPageRail(tree, groups)).toBe(true);
  });

  it('both agree false for a single ungrouped/legacy bucket', () => {
    const schema = [field({ key: 'a' }), field({ key: 'b' })];
    const tree = buildGroupTree(schema, undefined);
    expect(tree.length).toBe(1);
    expect(hasCardRail(tree, undefined)).toBe(false);
    expect(hasPageRail(tree, undefined)).toBe(false);
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
