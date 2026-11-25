/**
 * Tiny disposal registry.
 *
 * The SDK attaches DOM nodes and event listeners while `init()` is active. Every
 * one of them registers a disposer here so a single `dispose()` reliably tears
 * everything down (used by `destroy()` and by re-`init()`).
 *
 * Disposers run in reverse registration order (LIFO), and a throwing disposer
 * never prevents the rest from running.
 */

export type Disposer = () => void;

/** Anything with a `remove()` method (e.g. an `Element`). */
export interface Removable {
  remove(): void;
}

function safeCall(disposer: Disposer): void {
  try {
    disposer();
  } catch {
    // Tearing down must not throw; ignore and continue.
  }
}

export class Lifecycle {
  private readonly disposers: Disposer[] = [];
  private disposed = false;

  /** Registers a disposer. If already disposed, runs it immediately. */
  add(disposer: Disposer): void {
    if (this.disposed) {
      safeCall(disposer);
      return;
    }
    this.disposers.push(disposer);
  }

  /** Registers an element to be removed on dispose. */
  addElement(element: Removable): void {
    this.add(() => element.remove());
  }

  /** Whether {@link dispose} has already run. */
  isDisposed(): boolean {
    return this.disposed;
  }

  /** Runs every registered disposer exactly once, in reverse order. */
  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    const pending = this.disposers.splice(0).reverse();
    for (const disposer of pending) safeCall(disposer);
  }
}
