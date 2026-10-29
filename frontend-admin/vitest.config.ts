import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfigFn from './vite.config.js';

// The coverage run is the pre-push gate (`make ci` → `npm run test:coverage`).
// It instruments every file and often runs on a busy host, where heavy
// component tests then miss the 5 s test timeout and Testing Library's 1 s
// waits on files that pass alone, a different file each run. Wider limits there tolerate the
// slowness only — a test that is actually broken still fails. `npm test`
// keeps the defaults, so a test that needs these limits on a quiet machine
// still shows up.
const coverageRun = process.argv.includes('--coverage');

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
      testTimeout: coverageRun ? 15_000 : 5_000,
      // Read by src/test/setup.ts (Testing Library asyncUtilTimeout).
      env: { RTL_ASYNC_UTIL_TIMEOUT: coverageRun ? '3000' : '1000' },
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
