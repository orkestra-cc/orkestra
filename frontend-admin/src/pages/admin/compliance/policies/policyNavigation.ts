interface Loc {
  pathname: string;
  search: string;
}

// shouldBlockPolicyNavigation is the useBlocker predicate of the policy
// editor: unsaved changes are lost when the page changes or when ?from=
// (the policy a new one is copied from) changes, not when ?section= does —
// a section switch keeps the same form.
export function shouldBlockPolicyNavigation(
  dirty: boolean,
  current: Loc,
  next: Loc
): boolean {
  if (!dirty) return false;
  if (current.pathname !== next.pathname) return true;
  const from = (l: Loc) => new URLSearchParams(l.search).get('from');
  return from(current) !== from(next);
}
