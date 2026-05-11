import { defineConfig, mergeConfig } from 'vitest/config';
import path from 'path';
import viteConfigFn from './vite.config.js';

export default mergeConfig(
  viteConfigFn({ mode: 'test' }),
  defineConfig({
    // react-router-dom v7 ships its own dist that re-exports from
    // react-router. In Vitest, dedupe doesn't collapse them, so a Router
    // context from one package can't be read by hooks imported from the
    // other. Force every import to land on the same react-router copy.
    resolve: {
      alias: {
        'react-router-dom': path.resolve(__dirname, 'node_modules/react-router'),
      },
    },
    test: {
      globals: true,
      // happy-dom over jsdom: 2-3x faster and avoids the
      // "RequestInit: Expected signal to be an instance of AbortSignal"
      // mismatch jsdom + MSW v2 + Node fetch trip over.
      environment: 'happy-dom',
      setupFiles: ['./src/test/setup.ts'],
      include: ['src/**/*.{test,spec}.{ts,tsx}'],
      coverage: {
        provider: 'v8',
        // json-summary is what the CI badge-refresh step parses
        // (coverage/coverage-summary.json). lcov is for IDE plugins;
        // text is the human summary that lands in the job log.
        reporter: ['text', 'lcov', 'json-summary'],
        include: ['src/**/*.{ts,tsx}'],
        exclude: ['src/reference/**', 'src/modules/_template/**', 'src/test/**'],
      },
    },
  })
);
