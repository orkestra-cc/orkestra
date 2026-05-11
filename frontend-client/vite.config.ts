import path from 'node:path';
import { fileURLToPath } from 'node:url';
import type { IncomingMessage, ServerResponse } from 'node:http';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

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
    if (req.url === '/health' || req.url === '/health/') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(
        JSON.stringify({
          status: 'healthy',
          service: 'orkestra-client',
          timestamp: new Date().toISOString(),
        }),
      );
      return;
    }
    next();
  };
  return {
    name: 'orkestra-client-health-check',
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
const allowedHosts = (process.env.VITE_ALLOWED_HOSTS ?? '')
  .split(',')
  .map((h) => h.trim())
  .filter(Boolean);

export default defineConfig({
  plugins: [react(), tailwindcss(), healthCheckPlugin()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
    allowedHosts: allowedHosts.includes('*') ? true : allowedHosts,
  },
  preview: {
    host: '0.0.0.0',
    port: 5173,
    allowedHosts: allowedHosts.includes('*') ? true : allowedHosts,
  },
});
