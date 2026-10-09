// src/components/common/SortableGroupList/useCollapsedSet.ts
// Collapsed-group state persisted per list in localStorage. Controlled input
// for SortableGroupList (`collapsed` + `onToggle={toggle}`); `expandAll` /
// `collapseAll` exist for pages with a "view" menu.

import { useCallback, useState } from 'react';

const read = (key: string): Set<string> => {
  try {
    const raw = localStorage.getItem(key);
    return new Set<string>(raw ? (JSON.parse(raw) as string[]) : []);
  } catch {
    return new Set<string>();
  }
};

const write = (key: string, s: Set<string>) => {
  try {
    localStorage.setItem(key, JSON.stringify([...s]));
  } catch {
    /* quota exceeded / storage disabled: the in-memory state still works */
  }
};

export const useCollapsedSet = (storageKey: string) => {
  const [collapsed, setCollapsed] = useState<Set<string>>(() =>
    read(storageKey)
  );

  const commit = useCallback(
    (next: Set<string>) => {
      write(storageKey, next);
      setCollapsed(next);
    },
    [storageKey]
  );

  const toggle = useCallback(
    (id: string) => {
      setCollapsed(prev => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        write(storageKey, next);
        return next;
      });
    },
    [storageKey]
  );

  const expandAll = useCallback(() => commit(new Set<string>()), [commit]);
  const collapseAll = useCallback(
    (ids: string[]) => commit(new Set(ids)),
    [commit]
  );

  return { collapsed, toggle, expandAll, collapseAll };
};
