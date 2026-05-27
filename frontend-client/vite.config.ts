import { execSync } from "node:child_process";
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
    return execSync("git describe --tags --always --dirty", {
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
  plugins: [react(), tailwindcss(), healthCheckPlugin()],
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
