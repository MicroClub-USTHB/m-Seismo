// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest';
import { Lifecycle } from '@sdk/core/lifecycle';

describe('Lifecycle', () => {
  it('runs disposers in reverse registration order', () => {
    const order: number[] = [];
    const lifecycle = new Lifecycle();
    lifecycle.add(() => order.push(1));
    lifecycle.add(() => order.push(2));
    lifecycle.add(() => order.push(3));
    lifecycle.dispose();
    expect(order).toEqual([3, 2, 1]);
  });

  it('is idempotent (disposers run once)', () => {
    const disposer = vi.fn();
    const lifecycle = new Lifecycle();
    lifecycle.add(disposer);
    lifecycle.dispose();
    lifecycle.dispose();
    expect(disposer).toHaveBeenCalledTimes(1);
    expect(lifecycle.isDisposed()).toBe(true);
  });

  it('runs a disposer immediately when added after dispose', () => {
    const lifecycle = new Lifecycle();
    lifecycle.dispose();
    const late = vi.fn();
    lifecycle.add(late);
    expect(late).toHaveBeenCalledTimes(1);
  });

  it('a throwing disposer does not stop the rest', () => {
    const before = vi.fn();
    const after = vi.fn();
    const lifecycle = new Lifecycle();
    lifecycle.add(before);
    lifecycle.add(() => {
      throw new Error('boom');
    });
    lifecycle.add(after);
    expect(() => lifecycle.dispose()).not.toThrow();
    expect(before).toHaveBeenCalledTimes(1);
    expect(after).toHaveBeenCalledTimes(1);
  });

  it('addElement removes the element on dispose', () => {
    const element = { removed: false, remove(): void { this.removed = true; } };
    const lifecycle = new Lifecycle();
    lifecycle.addElement(element);
    lifecycle.dispose();
    expect(element.removed).toBe(true);
  });
});
