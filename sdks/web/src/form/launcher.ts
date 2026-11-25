/**
 * Floating launcher button.
 *
 * A single, self-contained button appended to `document.body`. It carries a
 * stable `data-mseismo-launcher` attribute and uses inline positioning so the
 * host page's CSS cannot move or hide it unexpectedly.
 */
import type { LauncherConfig } from '../core/config';

export function createLauncher(
  config: Extract<LauncherConfig, { enabled: true }>,
  onClick: () => void,
): HTMLButtonElement {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = config.label;
  button.setAttribute('data-mseismo-launcher', '');
  button.setAttribute('data-testid', 'mseismo-launcher');
  button.setAttribute('aria-label', config.label);

  const styles: Record<string, string> = {
    position: 'fixed',
    'z-index': '2147483000',
    margin: '0',
    'min-height': '44px',
    padding: '10px 16px',
    border: '1px solid rgba(0,0,0,0.12)',
    'border-radius': '999px',
    background: '#2f6fed',
    color: '#ffffff',
    font: "600 14px/1 system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
    cursor: 'pointer',
    'box-shadow': '0 4px 14px rgba(0,0,0,0.25)',
  };

  switch (config.position) {
    case 'bottom-right':
      styles['right'] = '20px';
      styles['bottom'] = '20px';
      break;
    case 'bottom-left':
      styles['left'] = '20px';
      styles['bottom'] = '20px';
      break;
    case 'top-right':
      styles['right'] = '20px';
      styles['top'] = '20px';
      break;
    case 'top-left':
      styles['left'] = '20px';
      styles['top'] = '20px';
      break;
  }

  for (const [property, value] of Object.entries(styles)) {
    button.style.setProperty(property, value);
  }

  // Progressive, no-dependency polish. Injected once into <head> (id-guarded)
  // so it never changes the button's textContent or accessible name. The rules
  // only upgrade focus, motion and High-Contrast looks when supported.
  const STYLE_ID = 'mseismo-launcher-style';
  if (document.getElementById(STYLE_ID) === null && document.head !== null) {
    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = `
      [data-mseismo-launcher]:hover { filter: brightness(1.05); }
      [data-mseismo-launcher]:focus-visible {
        outline: 3px solid #ffffff; outline-offset: 2px;
        box-shadow: 0 0 0 5px rgba(47, 111, 237, 0.6);
      }
      @media (prefers-reduced-motion: reduce) {
        [data-mseismo-launcher] { transition: none !important; }
      }
      @media (forced-colors: active) {
        [data-mseismo-launcher] {
          background: Highlight; color: HighlightText; border: 1px solid Highlight;
        }
        [data-mseismo-launcher]:focus-visible { outline: 3px solid Highlight; }
      }
    `;
    document.head.appendChild(style);
  }

  button.addEventListener('click', () => onClick());
  return button;
}
