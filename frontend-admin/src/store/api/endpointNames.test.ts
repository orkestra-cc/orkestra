// Every RTK Query slice in this app injects into ONE shared `baseApi`, so the
// endpoint name is a global identifier — not a per-file one. `injectEndpoints`
// refuses to overwrite a name that is already registered: it logs and
// `continue`s, silently DROPPING the second definition (see
// @reduxjs/toolkit's buildCreateApi → injectEndpoints). Both slices then export
// a `use<Name>Query` hook that is the *same* hook, bound to whichever
// definition was evaluated first.
//
// The evaluation order is a race: `useModuleApiInjection` fires every enabled
// addon's `injectApi()` dynamic import in a loop without awaiting, and lazy
// page chunks import their slice too. So a collision produces a page that
// loads its data on some page loads and silently shows an empty list on
// others — "reload and it works". That is exactly how the calendar events list
// failed: `calendarApi.listEvents` and `formsApi.listEvents` collided, and on
// the loads forms won, /calendar queried /v1/admin/forms/events and rendered
// zero events.
//
// Page-level tests cannot catch this — they import one addon's slice, so the
// name is free and the collision never happens. It only appears when two
// addons are enabled together at runtime. Hence this source-level guard, in
// the same spirit as modules/no-addon-strays.test.ts.
import { describe, it, expect } from 'vitest';

// Every file that injects endpoints: the api slices plus the two that live
// outside store/api (hooks/useSettings.ts, modules/_template/api.ts). Read as
// raw text at build time — evaluating them would inject for real and defeat
// the point of a static check.
const sources = import.meta.glob(
  ['./*.ts', '../../hooks/**/*.ts', '../../modules/**/*.ts'],
  { eager: true, query: '?raw', import: 'default' }
) as Record<string, string>;

// `    name: build.query<...>` / `builder.mutation(...)`, at any indent depth
// of 4+ (the endpoints object is always nested inside injectEndpoints).
const ENDPOINT_RE =
  /^\s{4,}([A-Za-z0-9_]+):\s*(?:build|builder)\.(?:query|mutation|infiniteQuery)[<(]/gm;

// Match the CALL, not the bare word: `modules/types.ts` names injectEndpoints
// in a doc comment without declaring a single endpoint, and a substring test
// counts it as a slice that mysteriously contributes nothing.
const INJECT_CALL_RE = /\.injectEndpoints\s*\(/;

const slicePaths = (): string[] =>
  Object.entries(sources)
    .filter(
      ([path, src]) => !path.includes('.test.') && INJECT_CALL_RE.test(src)
    )
    .map(([path]) => path);

const collectEndpoints = (): Map<string, string[]> => {
  const byName = new Map<string, string[]>();
  for (const path of slicePaths()) {
    for (const m of sources[path].matchAll(ENDPOINT_RE)) {
      const name = m[1];
      byName.set(name, [...(byName.get(name) ?? []), path]);
    }
  }
  return byName;
};

describe('RTK Query endpoint names', () => {
  it('are unique across every slice injected into baseApi', () => {
    const duplicates = [...collectEndpoints().entries()]
      .filter(([, paths]) => paths.length > 1)
      .map(([name, paths]) => `${name} → ${paths.sort().join(', ')}`);

    expect(duplicates).toEqual([]);
  });

  // Guards the guard: a regex that silently stopped matching would make the
  // assertion above vacuously true. Asserted per FILE rather than as a total
  // count — a threshold would have to be recalibrated for every repo in the
  // chain (the core-only base declares far fewer endpoints than a fork
  // carrying addons), and a global count only catches the regex breaking
  // everywhere at once. Per file it also catches it breaking on one slice's
  // formatting.
  it('are collected from every slice that injects endpoints', () => {
    const contributing = new Set([...collectEndpoints().values()].flat());
    const silent = slicePaths().filter(p => !contributing.has(p));

    expect(silent).toEqual([]);
  });
});
