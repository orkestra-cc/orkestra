import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfigFn from './vite.config.js';

export default mergeConfig(
  viteConfigFn({ mode: 'test' }),
  defineConfig({
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
        exclude: [
          'src/reference/**',
          'src/modules/_template/**',
          'src/test/**'
        ],
        // Floor set at current numbers (3.25 / 33.16 / 15.22 / 3.25) rounded
        // down a hair so trivial fluctuation doesn't redden CI. Ratchet up
        // when new test files land — never down. Drops force a conversation.
        thresholds: {
          statements: 3,
          branches: 33,
          functions: 15,
          lines: 3
        }
      }
    }
  })
);
