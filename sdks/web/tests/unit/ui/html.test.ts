/**
 * Escaping, JSON highlighting, byte formatting and shallow diff helpers used by
 * the test-app UI. The escaping tests double as the XSS safety contract: any
 * hostile value must survive as inert text.
 */
import { describe, expect, it } from 'vitest';
import { diffJson, escapeHtml, formatKilobytes, highlightJson } from '@app/ui/html';

describe('escapeHtml', () => {
  it('escapes the five HTML-significant characters', () => {
    expect(escapeHtml(`<&>"'`)).toBe('&lt;&amp;&gt;&quot;&#39;');
  });

  it('neutralizes a script tag', () => {
    const hostile = '<script>window.__xss=1</script>';
    const escaped = escapeHtml(hostile);
    expect(escaped).not.toContain('<script>');
    expect(escaped).toContain('&lt;script&gt;');
  });

  it('neutralizes an img onerror payload', () => {
    const escaped = escapeHtml('<img src=x onerror="alert(1)">');
    expect(escaped).not.toContain('<img');
    expect(escaped).not.toContain('"');
  });

  it('stringifies non-strings without throwing', () => {
    expect(escapeHtml(42)).toBe('42');
    expect(escapeHtml(null)).toBe('null');
    expect(escapeHtml(undefined)).toBe('undefined');
  });
});

describe('highlightJson', () => {
  it('wraps keys, strings, numbers and literals in spans', () => {
    const html = highlightJson('{"a":"b","n":1,"t":true,"z":null}');
    expect(html).toContain('<span class="j-key">&quot;a&quot;</span>');
    expect(html).toContain('<span class="j-str">&quot;b&quot;</span>');
    expect(html).toContain('<span class="j-num">1</span>');
    expect(html).toContain('<span class="j-lit">true</span>');
    expect(html).toContain('<span class="j-lit">null</span>');
  });

  it('escapes hostile strings inside the JSON', () => {
    const html = highlightJson(JSON.stringify({ evil: '<script>x</script>' }));
    expect(html).not.toContain('<script>');
    expect(html).toContain('&lt;script&gt;');
  });

  it('escapes HTML-looking values even when the JSON itself contains quotes', () => {
    const html = highlightJson(JSON.stringify({ a: '"onmouseover="alert(1)' }));
    expect(html).not.toContain('onmouseover="alert');
  });

  it('preserves the round-trippable text content of the JSON', () => {
    const json = '{"a":[1,2,3],"b":"x\\ny"}';
    const stripped = highlightJson(json).replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&').replace(/&quot;/g, '"').replace(/&#39;/g, "'");
    expect(stripped).toBe(json);
  });

  it('handles exponential and negative numbers', () => {
    expect(highlightJson('{"n":-1.5e10}')).toContain('<span class="j-num">-1.5e10</span>');
  });
});

describe('formatKilobytes', () => {
  it('shows bytes below 1 KB', () => {
    expect(formatKilobytes(512)).toBe('512 B');
  });

  it('shows one-decimal KB at and above 1 KB', () => {
    expect(formatKilobytes(2048)).toBe('2.0 KB');
  });
});

describe('diffJson', () => {
  it('returns no rows for identical objects regardless of key order', () => {
    expect(diffJson({ a: 1, b: 2 }, { b: 2, a: 1 })).toEqual([]);
  });

  it('reports changed leaf paths', () => {
    const rows = diffJson({ a: 1, nested: { b: 'x' } }, { a: 2, nested: { b: 'x' } });
    expect(rows).toHaveLength(1);
    expect(rows[0]!.path).toBe('a');
    expect(rows[0]!.left).toBe('1');
    expect(rows[0]!.right).toBe('2');
  });

  it('reports added and removed keys with undefined on the missing side', () => {
    const rows = diffJson({ a: 1 }, { a: 1, b: 2 });
    expect(rows).toHaveLength(1);
    expect(rows[0]!.path).toBe('b');
    expect(rows[0]!.left).toBe('undefined');
    expect(rows[0]!.right).toBe('2');
  });

  it('descends into nested objects', () => {
    const rows = diffJson({ x: { y: { z: 1 } } }, { x: { y: { z: 2 } } });
    expect(rows[0]!.path).toBe('x.y.z');
  });
});
