import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  resolve: {
    alias: {
      '@m-seismo/web': fileURLToPath(new URL('./src/index.ts', import.meta.url)),
      '@sdk': fileURLToPath(new URL('./src', import.meta.url)),
      '@app': fileURLToPath(new URL('./test-app/src', import.meta.url)),
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
    coverage: {
      provider: 'v8',
      // Vitest 4: `include` (not the removed `all` flag) decides which files
      // are reported, so every source file is listed whether or not a test
      // imports it.
      include: ['src/**/*.ts'],
      exclude: [
        // Public entry: only re-exports and thin wrappers.
        'src/index.ts',
        // Test-only inspection accessor (never shipped; used by the test app
        // and Playwright to read the diagnostics buffer).
        'src/internal.ts',
        // Barrel re-export modules with no logic of their own.
        'src/core/index.ts',
        'src/context/index.ts',
        'src/form/index.ts',
        'src/privacy/index.ts',
        'src/transport/index.ts',
        // Pure type-only modules: erased at compile time, nothing to execute.
        'src/transport/types.ts',
        'src/core/events.ts',
        // Test-app / lab harness (not shippable SDK code).
        'test-app/**',
      ],
      reporter: ['text', 'html'],
      reportsDirectory: 'coverage',
      // Thresholds are the measured floor rounded down (see
      // report/phase-tests-unit-and-e2e.md). The DOM-heavy directories
      // (form/collectors/network/reporting) get a lower bar than the pure
      // logic directories, which get a higher one.
      thresholds: {
        statements: 89,
        branches: 80,
        functions: 94,
        lines: 92,
        'src/core/**.ts': { statements: 88, branches: 80, functions: 99, lines: 90 },
        'src/privacy/**.ts': { statements: 90, branches: 86, functions: 99, lines: 93 },
        'src/diagnostics/**.ts': { statements: 82, branches: 74, functions: 84, lines: 85 },
        'src/errors/**.ts': { statements: 92, branches: 86, functions: 99, lines: 91 },
        'src/context/**.ts': { statements: 90, branches: 83, functions: 99, lines: 91 },
        'src/transport/**.ts': { statements: 92, branches: 88, functions: 99, lines: 94 },
        'src/form/**.ts': { statements: 93, branches: 76, functions: 94, lines: 95 },
        'src/collectors/**.ts': { statements: 86, branches: 74, functions: 94, lines: 93 },
        'src/network/**.ts': { statements: 84, branches: 77, functions: 88, lines: 85 },
        'src/reporting/**.ts': { statements: 90, branches: 79, functions: 96, lines: 92 },
      },
    },
  },
});
