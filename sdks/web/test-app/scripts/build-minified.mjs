/**
 * Generates the tiny "minified production" bundles used by lab scenario E13.
 *
 * E13 proves the SDK performs NO source-map resolution: an error thrown from a
 * single-line bundle (with and without a `sourceMappingURL`) is reported with
 * frames pointing at the minified file. We do not need a real bundler — the
 * bundles are deliberately one line and the mapped variant carries a source map
 * whose `sources` reference a non-existent `.src.` file, so a stack frame that
 * ever referenced `.src.` would prove the SDK (or browser) resolved the map.
 *
 * Output (served by Vite from `test-app/public`):
 *   /minified/plain.js       — one-line bundle, no source map
 *   /minified/mapped.js      — one-line bundle + `//# sourceMappingURL=mapped.js.map`
 *   /minified/mapped.js.map  — a source map whose `sources` point at `plain.src.js`
 *
 * Usage: `npm run lab:minify` (also runs automatically before the lab/e2e start).
 */
import { mkdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const outDir = fileURLToPath(new URL('../public/minified/', import.meta.url));
mkdirSync(outDir, { recursive: true });

const plainSource = 'function labMinifiedPlain(){var a=1;var b=2;throw new Error("minified plain");}labMinifiedPlain();';
const mappedSource = 'function labMinifiedMapped(){var a=1;var b=2;throw new Error("minified mapped");}labMinifiedMapped();';

const plainBundle = `${plainSource}\n`;
const mappedBundle = `${mappedSource}\n//# sourceMappingURL=mapped.js.map\n`;

const map = {
  version: 3,
  file: 'mapped.js',
  sources: ['plain.src.js'],
  sourcesContent: [
    'function labMinifiedMapped() {\n  const a = 1;\n  const b = 2;\n  throw new Error("minified mapped");\n}\nlabMinifiedMapped();\n',
  ],
  names: [],
  mappings: '',
};

writeFileSync(path.join(outDir, 'plain.js'), plainBundle, 'utf8');
writeFileSync(path.join(outDir, 'mapped.js'), mappedBundle, 'utf8');
writeFileSync(path.join(outDir, 'mapped.js.map'), `${JSON.stringify(map, null, 2)}\n`, 'utf8');

// eslint-disable-next-line no-console -- build script progress.
console.log(`[lab-minify] wrote plain.js, mapped.js and mapped.js.map to ${outDir}`);
