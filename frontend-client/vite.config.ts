import { execSync } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { IncomingMessage, ServerResponse } from "node:http";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Mirror of the operator console's version resolver — single source of
// truth is the git tag. GITHUB_REF_NAME wins on tag-push workflows,
// ORKESTRA_VERSION is an ad-hoc override, then `git describe`, then a
// "dev" fallback for environments without git.
const resolveAppVersion = (): string => {
  const ref = process.env.GITHUB_REF_NAME;
  if (ref && /^v\d/.test(ref)) return ref.replace(/^v/, "");
  if (process.env.ORKESTRA_VERSION) return process.env.ORKESTRA_VERSION;
  try {
    // --match 'v[0-9]*': UPSTREAM tags only. A clone's own release tags
    // ("<clone>-v*", e.g. commons-v0.1.0) must not shadow the base version.
    return execSync("git describe --tags --match 'v[0-9]*' --always --dirty", {
      stdio: ["ignore", "pipe", "ignore"],
      cwd: __dirname,
    })
      .toString()
      .trim()
      .replace(/^v/, "");
  } catch {
    return "dev";
  }
};
const APP_VERSION = resolveAppVersion();

// LAN liveness probe for HAProxy / k8s. Same pattern the operator
// console uses — surfaces /health as a Vite dev/preview middleware
// rather than a route, so it short-circuits before the module pipeline
// and never returns 500 from a transform error.
const healthCheckPlugin = (): Plugin => {
  const handler = (
    req: IncomingMessage,
    res: ServerResponse,
    next: (err?: unknown) => void,
  ) => {
    if (req.url === "/health" || req.url === "/health/") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          status: "healthy",
          service: "orkestra-client",
          timestamp: new Date().toISOString(),
          version: APP_VERSION,
        }),
      );
      return;
    }
    next();
  };
  return {
    name: "orkestra-client-health-check",
    configureServer(server) {
      server.middlewares.use(handler);
    },
    configurePreviewServer(server) {
      server.middlewares.use(handler);
    },
  };
};

// /config.js holds the runtime config the SPA reads at boot
// (window.__ORKESTRA_CONFIG__ — apiBase, stripePublishableKey). Every
// environment rewrites it at container start, so a client holding a stale
// copy talks to the wrong backend. The production nginx image already says
// so (Dockerfile: `location = /config.js { add_header Cache-Control
// "no-store" }`); the dev server, which is what dev AND staging run, did
// not. Vite's public-dir middleware serves the file with `Cache-Control:
// no-cache`, which means "store it, but revalidate" — enough for a CDN to
// keep a copy, since Cloudflare classifies `.js` as a static asset by
// extension and hands browsers its own default `max-age` in place of the
// origin header. The operator console carries the same plugin; see
// frontend-admin/vite.config.js for the measured before/after.
//
// This serves the file itself instead of setting a header and deferring:
// sirv, the public-dir server, writes Cache-Control unconditionally into
// its response head, clobbering whatever an upstream middleware set.
const runtimeConfigPlugin = (): Plugin => {
  const handlerFor =
    (getDir: () => string) =>
    async (
      req: IncomingMessage,
      res: ServerResponse,
      next: (err?: unknown) => void,
    ) => {
      const [pathname] = (req.url ?? "").split("?");
      if (pathname !== "/config.js") {
        next();
        return;
      }
      let body: Buffer;
      try {
        body = await fs.readFile(path.join(getDir(), "config.js"));
      } catch (err) {
        // No config.js on disk — the documented failure mode after a git
        // operation deletes it under the bind mount. Fall through to the
        // old behaviour (Vite answers with index.html, which the browser
        // then blocks on nosniff), but name the cause first: that block
        // is otherwise a puzzle, and a silent fallback would also mean
        // the no-store header is quietly not applied.
        console.warn(
          `[orkestra-client-runtime-config] /config.js not served from disk: ${
            (err as Error).message
          }`,
        );
        next();
        return;
      }
      res.writeHead(200, {
        "Content-Type": "text/javascript",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      });
      res.end(body);
    };
  return {
    name: "orkestra-client-runtime-config",
    configureServer(server) {
      server.middlewares.use(handlerFor(() => server.config.publicDir));
    },
    configurePreviewServer(server) {
      // `vite preview` serves the built bundle, where config.js has been
      // copied into outDir — publicDir is not on the request path there.
      server.middlewares.use(
        handlerFor(() =>
          path.resolve(server.config.root, server.config.build.outDir),
        ),
      );
    },
  };
};

// VITE_ALLOWED_HOSTS — comma-separated list of hosts the dev server will
// answer to (Vite 5+ blocks unknown Host headers as a DNS-rebinding
// defence). Localhost is always allowed; this list adds the deployed
// hostnames (e.g. app.orkestra.cc on staging). Set to `*` to disable
// the check entirely (acceptable on a private VM, never in prod).
const allowedHosts = (process.env.VITE_ALLOWED_HOSTS ?? "")
  .split(",")
  .map((h) => h.trim())
  .filter(Boolean);

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(APP_VERSION),
  },
  plugins: [react(), tailwindcss(), healthCheckPlugin(), runtimeConfigPlugin()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    strictPort: true,
    allowedHosts: allowedHosts.includes("*") ? true : allowedHosts,
  },
  preview: {
    host: "0.0.0.0",
    port: 5173,
    allowedHosts: allowedHosts.includes("*") ? true : allowedHosts,
  },
});
