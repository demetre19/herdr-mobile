// Select-to-copy: any selection that settles inside an element marked
// `data-copy-on-select` lands on the clipboard automatically — the same
// "highlight it and it's copied" behaviour herdr's desktop terminal has.
// Mobile handle drags emit a burst of selectionchange events, so the copy
// waits for the selection to settle; browsers that refuse a clipboard write
// without a gesture get a one-shot retry on the next pointerup.

import { relayStore } from './store';

const SELECT_COPY_SELECTOR = '[data-copy-on-select]';
const SETTLE_MS = 400;
function markedRegion(selection: Selection): Element | null {
  // Both endpoints must sit in the same marked container: an anchor inside
  // the log and a focus dragged into the composer is not a copy gesture.
  const anchor = selection.anchorNode instanceof Element
    ? selection.anchorNode
    : selection.anchorNode?.parentElement;
  const focus = selection.focusNode instanceof Element
    ? selection.focusNode
    : selection.focusNode?.parentElement;
  const mark = anchor?.closest(SELECT_COPY_SELECTOR) || null;
  return mark && mark === focus?.closest(SELECT_COPY_SELECTOR) ? mark : null;
}
export function initializeSelectCopy(): () => void {
  let settleTimer: ReturnType<typeof setTimeout> | undefined;
  let retryOnGesture: (() => void) | undefined;
  let lastCopied = '';

  const armGestureRetry = (text: string) => {
    retryOnGesture?.();
    const cancel = () => {
      document.removeEventListener('pointerup', onGesture, true);
      document.removeEventListener('touchend', onGesture, true);
    };
    const finish = async () => {
      // touchend and the compat pointerup both fire on mobile — the first
      // removes the other so the write + toast happen exactly once.
      cancel();
      retryOnGesture = undefined;
      if (!navigator.clipboard?.writeText) return;
      try {
        await navigator.clipboard.writeText(text);
        lastCopied = text;
        relayStore.showToast('Copied.');
      } catch { /* A second refusal stays silent — the OS copy affordance is there. */ }
    };
    function onGesture() { void finish(); }
    retryOnGesture = cancel;
    document.addEventListener('pointerup', onGesture, { capture: true, once: true });
    document.addEventListener('touchend', onGesture, { capture: true, once: true });
  };

  const copySelection = async () => {
    retryOnGesture?.();
    retryOnGesture = undefined;
    const selection = document.getSelection();
    if (!selection || selection.isCollapsed || !markedRegion(selection)) return;
    const text = selection.toString();
    if (!text.trim() || text === lastCopied) return;
    if (!navigator.clipboard?.writeText) return;
    try {
      await navigator.clipboard.writeText(text);
      lastCopied = text;
      relayStore.showToast('Copied.');
    } catch {
      // Transient-activation users (Safari) reject here; the gesture that
      // released the handle is still imminent, so retry once on pointerup.
      armGestureRetry(text);
    }
  };

  const onSelectionChange = () => {
    clearTimeout(settleTimer);
    settleTimer = setTimeout(() => { void copySelection(); }, SETTLE_MS);
  };

  document.addEventListener('selectionchange', onSelectionChange);
  return () => {
    clearTimeout(settleTimer);
    retryOnGesture?.();
    document.removeEventListener('selectionchange', onSelectionChange);
  };
}
