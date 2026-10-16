import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  resolve: {
    alias: {
      '@m-seismo/web': fileURLToPath(new URL('./src/index.ts', import.meta.url)),
      '@sdk': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    globals: true,
    // Unit tests live in tests/unit and run in a plain Node environment.
    // The SDK source must be SSR-safe: importing it here must never touch
    // `window`/`document`. Note we do NOT set `environment: 'jsdom'` globally.
    // To opt a single file into a DOM environment later, add this pragma at the
    // top of that file (requires a DOM provider such as jsdom/happy-dom):
    //   // @vitest-environment jsdom
    environment: 'node',
    include: ['tests/unit/**/*.{test,spec}.ts'],
    exclude: ['tests/e2e/**', 'node_modules/**', 'dist/**'],
  },
});
