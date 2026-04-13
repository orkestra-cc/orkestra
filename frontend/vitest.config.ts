import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfigFn from './vite.config.js';

export default mergeConfig(
  viteConfigFn({ mode: 'test' }),
  defineConfig({
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      include: ['src/**/*.{test,spec}.{ts,tsx}'],
      coverage: {
        provider: 'v8',
        reporter: ['text', 'lcov'],
        include: ['src/**/*.{ts,tsx}'],
        exclude: ['src/reference/**', 'src/modules/_template/**', 'src/test/**'],
      },
    },
  })
);
