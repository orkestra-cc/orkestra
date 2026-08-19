// Dev-default runtime config template.
//
// public/config.js is gitignored and generated at container start by
// docker-compose.{dev,staging}.yml from the VITE_* env vars (see
// frontend-client/CLAUDE.md → "Runtime config"). This example is the
// fallback the app falls back to when the generator can't run — e.g.
// `npm run dev` directly on the host — and it documents the shape the
// SPA reads (window.__ORKESTRA_CONFIG__).
window.__ORKESTRA_CONFIG__ = {
  apiBase: "http://api.localhost:3000",
  stripePublishableKey: "",
};
