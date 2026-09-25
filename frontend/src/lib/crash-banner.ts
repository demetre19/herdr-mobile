/**
 * Phones have no console. An uncaught exception on the phone previously
 * vanished, leaving symptoms like a button that "does nothing" with no way to
 * see why. This banner is raw DOM on purpose: when the crash is inside the
 * reactive runtime, any store-driven toast can be wedged along with the app.
 */
const seen = new Set<string>();
let banner: HTMLElement | null = null;

function show(message: string): void {
  // One line of evidence is enough to report; the full stack still lands in
  // the banner's title for copy/paste, but the banner itself stays a strip.
  const text = message.trim().split('\n')[0].slice(0, 140);
  if (!text || seen.has(text) || seen.size >= 3) return;
  if (text.includes('ResizeObserver loop')) return;
  seen.add(text);
  try {
    if (!banner) {
      banner = document.createElement('div');
      banner.setAttribute('role', 'alert');
      // Bottom-anchored single strip: readable, dismissible, and small enough
      // that it never covers the workspace list it is reporting about.
      banner.style.cssText = 'position:fixed;bottom:0;left:0;right:0;z-index:2147483647;'
        + 'background:#7f1d1d;color:#fff;font:11px/1.3 monospace;padding:.35rem .6rem;'
        + 'white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-height:2.2rem;';
      banner.addEventListener('click', () => {
        banner?.remove();
        banner = null;
        seen.clear();
      });
      document.body.append(banner);
    }
    const prefix = banner.textContent ? `${banner.textContent} · ` : 'App error (tap to dismiss): ';
    banner.textContent = `${prefix}${text}`;
    banner.title = `${message.trim().slice(0, 2000)}`;
  } catch {
    // Reporting must never take the app down further.
  }
}

/**
 * Reports an invariant violation the app survived. It rides the same banner
 * as a crash because both mean the same thing to a phone with no console:
 * something is wrong and this text is the only evidence that will exist.
 */
export function reportAppAnomaly(message: string): void {
  show(message);
}

let handlersInstalled = false;
export function reportUncaughtErrors(): void {
  if (handlersInstalled) return;
  handlersInstalled = true;
  window.addEventListener('error', (event) => {
    // Browsers hide everything but "Script error." for scripts from another
    // origin, which on a phone means a Safari extension or content blocker,
    // never the app's own same-origin module. There is nothing to show.
    if (event.message === 'Script error.' && !event.filename) return;
    show(String(event.error?.stack || event.message || event.error || 'Unknown error'));
  });
  window.addEventListener('unhandledrejection', (event) => {
    const reason = event.reason as { stack?: string } | undefined;
    show(`Unhandled rejection: ${String(reason?.stack || reason || 'unknown')}`);
  });
}
