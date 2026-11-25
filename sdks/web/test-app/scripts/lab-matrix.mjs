/**
 * Aggregates the per-browser lab results into a single results matrix.
 *
 * Reads `report/lab-results/<browser>.json` (written by tests/e2e/lab.spec.ts)
 * and writes `report/lab-results/matrix.md`. Missing browsers are shown as "–".
 *
 * Usage: `npm run lab:matrix`
 */
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const resultsDir = fileURLToPath(new URL('../../report/lab-results/', import.meta.url));

if (!existsSync(resultsDir)) {
  // eslint-disable-next-line no-console -- CLI feedback.
  console.error('[lab-matrix] no report/lab-results directory; run `npm run test:lab` first.');
  process.exit(1);
}

const files = readdirSync(resultsDir).filter((name) => name.endsWith('.json'));
if (files.length === 0) {
  // eslint-disable-next-line no-console -- CLI feedback.
  console.error('[lab-matrix] no per-browser JSON files; run `npm run test:lab` first.');
  process.exit(1);
}

const byBrowser = new Map();
for (const file of files) {
  const browser = path.basename(file, '.json');
  byBrowser.set(browser, JSON.parse(readFileSync(path.join(resultsDir, file), 'utf8')));
}

const browsers = Array.from(byBrowser.keys()).sort();
const allIds = [];
const meta = new Map();
for (const results of byBrowser.values()) {
  for (const result of results) {
    if (!meta.has(result.id)) {
      allIds.push(result.id);
      meta.set(result.id, result);
    }
  }
}
allIds.sort();

const ICON = { pass: '✅', info: 'ℹ️', fail: '❌', 'needs-manual': '🖐' };

function cell(result) {
  if (result === undefined) return '–';
  return ICON[result.status] ?? result.status;
}

const lines = [];
lines.push('# Error-laboratory results matrix');
lines.push('');
lines.push(`Generated from ${files.length} browser file(s): ${browsers.join(', ')}.`);
lines.push('Legend: ✅ pass · ℹ️ info (expected gap) · ❌ fail · 🖐 needs manual · – not run');
lines.push('');
lines.push(`| ID | Scenario | Expected | ${browsers.join(' | ')} |`);
lines.push(`| --- | --- | --- | ${browsers.map(() => '---').join(' | ')} |`);
for (const id of allIds) {
  const info = meta.get(id);
  const cells = browsers.map((browser) => {
    const result = (byBrowser.get(browser) ?? []).find((entry) => entry.id === id);
    return cell(result);
  });
  lines.push(`| ${id} | ${info.title} | ${info.expected} | ${cells.join(' | ')} |`);
}
lines.push('');

const failures = [];
for (const [browser, results] of byBrowser) {
  for (const result of results) {
    if (result.status === 'fail') failures.push(`${browser}/${result.id}: ${result.observed}`);
  }
}
lines.push('## Failures');
lines.push('');
lines.push(failures.length === 0 ? 'None.' : failures.map((line) => `- ${line}`).join('\n'));
lines.push('');

mkdirSync(resultsDir, { recursive: true });
writeFileSync(path.join(resultsDir, 'matrix.md'), `${lines.join('\n')}\n`, 'utf8');
// eslint-disable-next-line no-console -- CLI feedback.
console.log(`[lab-matrix] wrote ${path.join(resultsDir, 'matrix.md')} (${allIds.length} scenarios, ${browsers.length} browsers)`);
