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
window.__ORKESTRA_CONFIG__ = {
  apiUrl: 'http://console.localhost:3000',
  wsUrl: 'ws://console.localhost:3000/ws',
  env: 'development',
  debug: true
};
