/**
 * Ring of recent composer/IME events for diagnosing Android text-entry
 * corruption (duplicated characters, remounts mid-composition). Same
 * pattern as dictation.ts's herdr_dictation_trace — the mic button's
 * long-press copies both rings out together.
 */
export function inputTrace(entry: Record<string, unknown>): void {
  try {
    const list: unknown[] = JSON.parse(localStorage.getItem('herdr_input_trace') || '[]');
    list.push({ t: Date.now(), ...entry });
    localStorage.setItem('herdr_input_trace', JSON.stringify(list.slice(-60)));
  } catch { /* storage unavailable */ }
}
