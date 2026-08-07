import { Storage } from 'happy-dom';

// Restores `localStorage` / `sessionStorage` under Node >= 25.
//
// Node 25 introduced global Web Storage bindings, defined on globalThis as own
// accessors that emit an ExperimentalWarning and evaluate to `undefined` unless
// the process was started with `--localstorage-file`. Those own properties
// pre-empt the ones vitest's happy-dom environment would otherwise install, so
// on a newer Node both globals read back undefined — and so does
// `window.localStorage`, which resolves to the same binding in this
// environment. happy-dom does implement Web Storage; it simply never gets to
// put it on the global.
//
// Everything touching storage then dies with "Cannot read properties of
// undefined", which took out three suites here. Two call `localStorage.clear()`
// in a `beforeEach` and fail with that message directly. The third,
// `setup/steps/OrgStep`, fails as a timeout that names none of this: its
// `setMemberships` reducer persists the current org through
// `window.localStorage`, so the dispatch throws, `onNext` is never called, and
// the assertion simply waits forever.
//
// The repo pins Node 24 (.mise.toml), where none of this happens, so CI was
// never affected — but a contributor on a newer Node met a red suite that had
// nothing to do with their change. Restoring the globals costs less than
// policing a Node version, and stops the eventual pin bump from re-breaking
// three unrelated suites.
//
// Guarded, so on Node 24 happy-dom's own implementation is left untouched, and
// imported for its side effect as the FIRST import in setup.ts — ES imports are
// hoisted, so a plain statement there would run after every other import had
// already had its chance to touch storage while initialising.
for (const key of ['localStorage', 'sessionStorage'] as const) {
  if (globalThis[key]) continue;
  Object.defineProperty(globalThis, key, {
    value: new Storage(),
    configurable: true,
    writable: true
  });
}
