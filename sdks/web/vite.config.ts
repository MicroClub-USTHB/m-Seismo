import { defineConfig, type Plugin } from 'vite';
import { fileURLToPath } from 'node:url';

/**
 * Tiny mock API used by the test app. Two endpoints:
 *   GET /api/ok   -> 200 JSON
 *   GET /api/fail -> 500 JSON
 * These exist only so we can trigger a successful and a failing request from the
 * test app. The real backend contract is handled in a later phase.
 */
function mockApi(): Plugin {
  const handle = (path: string, status: number) => {
    return (
      _req: unknown,
      res: {
        statusCode: number;
        setHeader: (k: string, v: string) => void;
        end: (body?: string) => void;
      },
    ) => {
      void path;
      res.statusCode = status;
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify({ status, path }));
    };
  };

  return {
    name: 'm-seismo-mock-api',
    configureServer(server) {
      server.middlewares.use('/api/ok', handle('/api/ok', 200));
      server.middlewares.use('/api/fail', handle('/api/fail', 500));
    },
    configurePreviewServer(server) {
      server.middlewares.use('/api/ok', handle('/api/ok', 200));
      server.middlewares.use('/api/fail', handle('/api/fail', 500));
    },
  };
}

export default defineConfig({
  root: 'test-app',
  plugins: [mockApi()],
  resolve: {
    alias: {
      '@m-seismo/web': fileURLToPath(new URL('./src/index.ts', import.meta.url)),
      '@sdk': fileURLToPath(new URL('./src', import.meta.url)),
      '@app': fileURLToPath(new URL('./test-app/src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    lib: {
      entry: fileURLToPath(new URL('./src/index.ts', import.meta.url)),
      name: 'MSeismoWeb',
      formats: ['es'],
      fileName: () => 'm-seismo-web.js',
    },
  },
});
