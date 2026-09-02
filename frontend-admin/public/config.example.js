// Runtime config template — replaces build-time VITE_API_URL baking.
//
// `public/config.js` itself is **gitignored**. It is regenerated at
// container start by the docker-compose `command:` step (dev / staging)
// or by the nginx entrypoint `/docker-entrypoint.d/10-write-config.sh`
// (prod image). One published image works in dev / staging / prod —
// the SPA reads window.__ORKESTRA_CONFIG__ from /config.js at boot, so
// every environment sees its own URLs without a rebuild.
//
// If you run `npm run dev` directly on the host (outside Docker), copy
// this file once and edit as needed:
//
//   cp frontend-admin/public/config.example.js frontend-admin/public/config.js
//
// Adding a new field: declare it on RuntimeConfig in
// src/config/environment.ts, read it via the `config` singleton, and add
// the corresponding env-var fallback in (a) the nginx entrypoint, and
// (b) the dev + staging compose `command:` scripts. Never reach for
// `import.meta.env.VITE_*` from new code — those bake at build time.
//
// apiUrl/wsUrl stay on `localhost`: `npm run dev` serves this SPA on
// localhost, every call carries credentials:'include', and the refresh
// cookie is SameSite=Lax — so the console's origin and its API must be the
// same site (spec §8 follow-up #13). `localhost:3000` reaches the operator
// mux through the dev fallthrough, so nothing has to be registered for it.
window.__ORKESTRA_CONFIG__ = {
  apiUrl: 'http://localhost:3000',
  wsUrl: 'ws://localhost:3000/ws',
  env: 'development',
  debug: true,
  version: 'dev',
  appName: 'orkestra',
  cloneVersion: 'dev',
  buildCommit: '',
  startedAt: ''
};
