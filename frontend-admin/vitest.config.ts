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
        // Re-anchored for vitest 4, which makes AST-aware remapping the
        // default. Nothing about the tests changed — the measurement did.
        // Proven by running the OLD vitest 3 with
        // `--coverage.experimentalAstAwareRemapping`, which reproduces the
        // new figures exactly: branches 73.06% -> 28.5%, lines 35.61% ->
        // 31.79%. The new numbers are the honest ones; the old provider
        // credited branches no test ever entered.
        //
        // `branches: 33` would now fail against a real 28.5% here. The
        // floors below are anchored to the WEAKEST suite in the fork chain,
        // not to this repo's own numbers: this file syncs between the public
        // base and its forks, and the base runs 294 tests against roughly the
        // same code the forks cover with 674 (branches 19.69% vs 28.49%).
        // Per-repo values would make this line conflict on every sync, so one
        // set that holds everywhere wins over a tighter gate that doesn't.
        thresholds: {
          statements: 3,
          branches: 17,
          functions: 12,
          lines: 3
        }
      }
    }
  })
);
