import { defineConfig, loadEnv } from 'vite';
import fs from 'fs/promises';
import path from 'path';
import { fileURLToPath } from 'url';
import react from '@vitejs/plugin-react';
import compileSCSS from './compile-scss';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// LAN liveness probe for HAProxy / k8s. Vite's `server.middlewares` is
// not a valid config key (the inline middleware that used to live there
// was silently ignored, and Vite's transform pipeline returned 500 on
// /health). Surfacing it as a plugin via configureServer is the
// supported path: the middleware runs before the dev server's module
// resolver, so /health short-circuits to a JSON 200 without ever
// reaching the import pipeline. Mirrored on configurePreviewServer so
// `vite preview` answers the same way.
const healthCheckPlugin = () => {
  const handler = (req, res, next) => {
    if (req.url === '/health' || req.url === '/health/') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(
        JSON.stringify({
          status: 'healthy',
          service: 'orkestra-frontend',
          timestamp: new Date().toISOString(),
          version: '0.3.0'
        })
      );
      return;
    }
    next();
  };
  return {
    name: 'orkestra-health-check',
    configureServer(server) {
      server.middlewares.use(handler);
    },
    configurePreviewServer(server) {
      server.middlewares.use(handler);
    }
  };
};

export default ({ mode }) => {
  process.env = { ...process.env, ...loadEnv(mode, process.cwd()) };

  return defineConfig({
    plugins: [react(), compileSCSS(), healthCheckPlugin()],
    base: process.env.VITE_PUBLIC_URL || '/',
    resolve: {
      extensions: ['.mjs', '.js', '.mts', '.ts', '.jsx', '.tsx', '.json'],
      // react-router-dom v7 re-exports react-router but ships a separate
      // dist; without dedupe Vite instantiates both as distinct modules,
      // so a <MemoryRouter> from react-router and a <Routes> from
      // react-router-dom won't share a Router context.
      dedupe: ['react-router', 'react-router-dom'],
      alias: {
        App: path.resolve(__dirname, './src/App'),
        components: path.resolve(__dirname, './src/components'),
        features: path.resolve(__dirname, './src/features'),
        pages: path.resolve(__dirname, './src/pages'),
        demos: path.resolve(__dirname, './src/demos'),
        docs: path.resolve(__dirname, './src/docs'),
        layouts: path.resolve(__dirname, './src/layouts'),
        providers: path.resolve(__dirname, './src/providers'),
        hooks: path.resolve(__dirname, './src/hooks'),
        helpers: path.resolve(__dirname, './src/helpers'),
        data: path.resolve(__dirname, './src/data'),
        assets: path.resolve(__dirname, './src/assets'),
        routes: path.resolve(__dirname, './src/routes'),
        reducers: path.resolve(__dirname, './src/reducers'),
        utils: path.resolve(__dirname, './src/utils'),
        widgets: path.resolve(__dirname, './src/widgets'),
        store: path.resolve(__dirname, './src/store'),
        stores: path.resolve(__dirname, './src/stores'),
        config: path.resolve(__dirname, './src/config'),
        reference: path.resolve(__dirname, './src/reference'),
        types: path.resolve(__dirname, './src/types'),
        modules: path.resolve(__dirname, './src/modules'),
        test: path.resolve(__dirname, './src/test')
      }
    },
    esbuild: {
      loader: 'tsx',
      include: /src\/.*\.[jt]sx?$/,
      exclude: []
    },
    optimizeDeps: {
      esbuildOptions: {
        plugins: [
          {
            name: 'load-js-files-as-tsx',
            setup(build) {
              build.onLoad({ filter: /src\/.*\.[jt]s$/ }, async args => ({
                loader: 'tsx',
                contents: await fs.readFile(args.path, 'utf8')
              }));
            }
          }
        ]
      }
    },
    build: {
      rollupOptions: {
        output: {
          manualChunks: {
            // React ecosystem
            'react-vendor': ['react', 'react-dom', 'react-router'],

            // Bootstrap and UI
            'ui-vendor': ['react-bootstrap', 'bootstrap', 'classnames'],

            // Charts and visualization
            'charts-vendor': ['echarts', 'echarts-for-react'],

            // Maps
            'maps-vendor': ['leaflet', 'react-leaflet', 'react-leaflet-markercluster'],

            // Form handling
            'forms-vendor': ['react-hook-form', '@hookform/resolvers', 'yup', 'react-select'],

            // Icons and media
            'icons-media-vendor': [
              '@fortawesome/fontawesome-svg-core',
              '@fortawesome/free-solid-svg-icons',
              '@fortawesome/free-regular-svg-icons',
              '@fortawesome/free-brands-svg-icons',
              '@fortawesome/react-fontawesome',
              'react-icons',
              'lottie-react'
            ],

            // Calendar and date
            'calendar-vendor': [
              '@fullcalendar/react',
              '@fullcalendar/daygrid',
              '@fullcalendar/timegrid',
              '@fullcalendar/list',
              '@fullcalendar/interaction',
              '@fullcalendar/bootstrap',
              'dayjs',
              'react-datepicker'
            ],

            // Editor and rich content
            'editor-vendor': ['@tinymce/tinymce-react', 'tinymce', 'prism-react-renderer'],

            // Utilities
            'utils-vendor': ['uuid', 'fuse.js', 'imask', 'react-imask']
          }
        }
      }
    },
    define: {
      global: 'window'
    },
    server: {
      // Pre-transform frequently visited pages on dev server start so the
      // first navigation doesn't pay the WSL2 cold-transform penalty (~3-5s).
      warmup: {
        clientFiles: [
          // Shared layout + navbar (every page depends on these)
          './src/layouts/MainLayout.tsx',
          './src/components/navbar/vertical/NavbarVertical.tsx',
          // All production pages — pre-transform so first navigation is instant
          './src/pages/**/index.tsx',
          './src/pages/user/dashboard/UserDashboard.tsx',
          './src/pages/user/settings/Settings.tsx',
          './src/pages/user/calendar/UserCalendar.tsx',
          './src/pages/operator/profile/OperatorProfile.tsx',
          './src/pages/admin/user-profile/AdminUserProfile.tsx',
        ]
      },
      open: false,
      port: 5173,
      host: '0.0.0.0',
      allowedHosts: [
        'localhost',
        '127.0.0.1',
        '192.168.88.53',
        'orkestra.cc',
        'staging.orkestra.cc'
      ],
      hmr: process.env.VITE_HMR_HOST
        ? {
            host: process.env.VITE_HMR_HOST,
            protocol: 'wss',
            clientPort: 443
          }
        : true,
      watch: {
        usePolling: true,
        interval: 300
      }
    }
  });
};
