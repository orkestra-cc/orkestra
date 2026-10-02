// src/components/common/SortableGroupList/reorder.test.ts
import { describe, expect, it } from 'vitest';
import {
  computeTargetOrder,
  droppableTypesFor,
  reorderIds,
  resolveDragEnd
} from './reorder';

describe('reorderIds', () => {
  it('moves active before over (downward → upward)', () => {
    expect(reorderIds(['a', 'b', 'c'], 'c', 'a')).toEqual(['c', 'a', 'b']);
  });
  it('moves active after over (upward → downward)', () => {
    expect(reorderIds(['a', 'b', 'c'], 'a', 'c')).toEqual(['b', 'c', 'a']);
  });
  it('returns the same array when an id is unknown', () => {
    const ids = ['a', 'b'];
    expect(reorderIds(ids, 'x', 'a')).toBe(ids);
  });
});

describe('computeTargetOrder', () => {
  it('inserts a cross-group row at the over position', () => {
    expect(computeTargetOrder(['a', 'b', 'c'], 'f9', 'b')).toEqual([
      'a',
      'f9',
      'b',
      'c'
    ]);
  });
  it('appends when overRowId is null (empty/collapsed group or band)', () => {
    expect(computeTargetOrder(['a', 'b'], 'f9', null)).toEqual([
      'a',
      'b',
      'f9'
    ]);
  });
  it('repositions within the same group without duplicating', () => {
    expect(computeTargetOrder(['a', 'b', 'c'], 'c', 'a')).toEqual([
      'c',
      'a',
      'b'
    ]);
  });
});

describe('droppableTypesFor', () => {
  it('scopes a group drag to group droppables only', () => {
    expect(droppableTypesFor('group')).toEqual(['group']);
  });
  it('scopes a row drag to rows and containers', () => {
    expect(droppableTypesFor('row')).toEqual(['row', 'container']);
  });
  it('defaults an undefined active type to the row scope', () => {
    expect(droppableTypesFor(undefined)).toEqual(['row', 'container']);
  });
});

describe('resolveDragEnd', () => {
  const groupIds = ['g1', 'g2'];
  const rows = { g1: ['a', 'b'], g2: ['c'] };

  it('returns null without an over target', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'a',
          activeType: 'row',
          overId: null,
          overType: undefined,
          overGroupId: undefined
        },
        groupIds,
        rows
      )
    ).toBeNull();
  });

  it('returns null on a self-drop', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'a',
          activeType: 'row',
          overId: 'a',
          overType: 'row',
          overGroupId: 'g1'
        },
        groupIds,
        rows
      )
    ).toBeNull();
  });

  it('reorders groups when a group lands on a group', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'g2',
          activeType: 'group',
          overId: 'g1',
          overType: 'group',
          overGroupId: undefined
        },
        groupIds,
        rows
      )
    ).toEqual({ kind: 'groups', ids: ['g2', 'g1'] });
  });

  it('ignores a group dropped on a row', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'g2',
          activeType: 'group',
          overId: 'a',
          overType: 'row',
          overGroupId: 'g1'
        },
        groupIds,
        rows
      )
    ).toBeNull();
  });

  it('ignores a group whose id is unknown', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'zz',
          activeType: 'group',
          overId: 'g1',
          overType: 'group',
          overGroupId: undefined
        },
        groupIds,
        rows
      )
    ).toBeNull();
  });

  it('moves a row onto another row: inserted at that row, same-group order authoritative', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'c',
          activeType: 'row',
          overId: 'b',
          overType: 'row',
          overGroupId: 'g1'
        },
        groupIds,
        rows
      )
    ).toEqual({
      kind: 'row',
      rowId: 'c',
      toGroupId: 'g1',
      ids: ['a', 'c', 'b']
    });
  });

  it('reorders within the same group', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'b',
          activeType: 'row',
          overId: 'a',
          overType: 'row',
          overGroupId: 'g1'
        },
        groupIds,
        rows
      )
    ).toEqual({ kind: 'row', rowId: 'b', toGroupId: 'g1', ids: ['b', 'a'] });
  });

  it('appends when a row lands on a band container', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'a',
          activeType: 'row',
          overId: 'grp:g2',
          overType: 'container',
          overGroupId: 'g2'
        },
        groupIds,
        rows
      )
    ).toEqual({ kind: 'row', rowId: 'a', toGroupId: 'g2', ids: ['c', 'a'] });
  });

  it('appends when a row lands on a group node', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'a',
          activeType: 'row',
          overId: 'g2',
          overType: 'group',
          overGroupId: undefined
        },
        groupIds,
        rows
      )
    ).toEqual({ kind: 'row', rowId: 'a', toGroupId: 'g2', ids: ['c', 'a'] });
  });

  it('appends to an empty destination group', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'a',
          activeType: 'row',
          overId: 'grp:g3',
          overType: 'container',
          overGroupId: 'g3'
        },
        [...groupIds, 'g3'],
        rows
      )
    ).toEqual({ kind: 'row', rowId: 'a', toGroupId: 'g3', ids: ['a'] });
  });

  it('returns null for an unknown active type', () => {
    expect(
      resolveDragEnd(
        {
          activeId: 'a',
          activeType: 'thing',
          overId: 'b',
          overType: 'row',
          overGroupId: 'g1'
        },
        groupIds,
        rows
      )
    ).toBeNull();
  });
});
