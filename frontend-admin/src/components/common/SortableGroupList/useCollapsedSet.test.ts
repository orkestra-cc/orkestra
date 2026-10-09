// src/components/common/SortableGroupList/useCollapsedSet.test.ts
import { describe, expect, it, beforeEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useCollapsedSet } from './useCollapsedSet';

const KEY = 'test.collapsed';

describe('useCollapsedSet', () => {
  beforeEach(() => localStorage.clear());

  it('starts empty and toggles an id in and out, persisting each change', () => {
    const { result } = renderHook(() => useCollapsedSet(KEY));
    expect(result.current.collapsed.size).toBe(0);

    act(() => result.current.toggle('a'));
    expect(result.current.collapsed.has('a')).toBe(true);
    expect(JSON.parse(localStorage.getItem(KEY) ?? '[]')).toEqual(['a']);

    act(() => result.current.toggle('a'));
    expect(result.current.collapsed.has('a')).toBe(false);
    expect(JSON.parse(localStorage.getItem(KEY) ?? '[]')).toEqual([]);
  });

  it('rehydrates from storage on mount', () => {
    localStorage.setItem(KEY, JSON.stringify(['x', 'y']));
    const { result } = renderHook(() => useCollapsedSet(KEY));
    expect([...result.current.collapsed]).toEqual(['x', 'y']);
  });

  it('expandAll clears; collapseAll sets exactly the given ids', () => {
    const { result } = renderHook(() => useCollapsedSet(KEY));
    act(() => result.current.collapseAll(['a', 'b']));
    expect([...result.current.collapsed]).toEqual(['a', 'b']);
    expect(JSON.parse(localStorage.getItem(KEY) ?? '[]')).toEqual(['a', 'b']);

    act(() => result.current.expandAll());
    expect(result.current.collapsed.size).toBe(0);
    expect(JSON.parse(localStorage.getItem(KEY) ?? '[]')).toEqual([]);
  });

  it('survives unparsable storage', () => {
    localStorage.setItem(KEY, '{not json');
    const { result } = renderHook(() => useCollapsedSet(KEY));
    expect(result.current.collapsed.size).toBe(0);
  });
});
