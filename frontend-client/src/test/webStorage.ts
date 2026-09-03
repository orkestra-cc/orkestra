import { Storage } from "happy-dom";

// Restores `localStorage` / `sessionStorage` under Node >= 25.
//
// Node 25 defines global Web Storage bindings as own accessors on
// globalThis that evaluate to `undefined` unless the process was started
// with `--localstorage-file`. Those own properties pre-empt the ones
// vitest's happy-dom environment would otherwise install, so on a newer
// Node both globals — and `window.localStorage`, the same binding here —
// read back undefined and every storage touch dies with "Cannot read
// properties of undefined". The repo pins Node 24 (.mise.toml), where none
// of this happens; the guard below is a no-op there and keeps a
// contributor on a newer Node from meeting a red suite unrelated to their
// change. Imported for its side effect as the FIRST import of setup.ts —
// ES imports are hoisted, so a plain statement would run too late.
// Mirrors frontend-admin/src/test/webStorage.ts.
for (const key of ["localStorage", "sessionStorage"] as const) {
  if (globalThis[key]) continue;
  Object.defineProperty(globalThis, key, {
    value: new Storage(),
    configurable: true,
    writable: true,
  });
}
