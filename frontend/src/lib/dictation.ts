import { get, writable } from 'svelte/store';

import { speechLanguage } from './speech';

/**
 * Voice dictation through the phone's own speech recognizer (Web Speech API).
 * Free and infrastructure-less: Android Chrome recognizes via the device's
 * speech service, desktop Chrome via its built-in recognizer. Nothing is
 * proxied through the relay.
 */

export type DictationState = 'off' | 'listening' | 'error';

interface SpeechRecognitionResultLike {
  isFinal: boolean;
  0: { transcript: string };
}

interface SpeechRecognitionEventLike {
  resultIndex: number;
  results: { length: number; [index: number]: SpeechRecognitionResultLike };
}

interface SpeechRecognitionLike {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onresult: ((event: SpeechRecognitionEventLike) => void) | null;
  onerror: ((event: { error: string }) => void) | null;
  onend: (() => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
}

type SpeechRecognitionCtor = new () => SpeechRecognitionLike;

function recognitionCtor(): SpeechRecognitionCtor | undefined {
  const w = window as unknown as Record<string, unknown>;
  return (w.SpeechRecognition || w.webkitSpeechRecognition) as SpeechRecognitionCtor | undefined;
}

export const dictationSupported = typeof window !== 'undefined' && Boolean(recognitionCtor());
export const dictationState = writable<DictationState>('off');

let recognition: SpeechRecognitionLike | undefined;
let wantListening = false;
// Text finalized in earlier sessions of this dictation run (Android Chrome
// ends the session on a pause and we restart it). The current session's
// transcript is rebuilt from event.results on every result — Android re-fires
// the whole cumulative text as one final result, so accumulating across
// events doubles every word.
let committed = '';
let sessionFinal = '';
let onText: ((finalText: string, interimText: string) => void) | undefined;
let onIssue: ((message: string) => void) | undefined;

// Opt-in trace for diagnosing Android recognizer behavior on real devices:
// `localStorage.setItem('herdr_dictation_debug','1')` before dictating; the
// last 40 events land in 'herdr_dictation_trace' for inspection.
function dictationTrace(entry: Record<string, unknown>): void {
  try {
    if (localStorage.getItem('herdr_dictation_debug') !== '1') return;
    const list: unknown[] = JSON.parse(localStorage.getItem('herdr_dictation_trace') || '[]');
    list.push({ t: Date.now(), ...entry });
    localStorage.setItem('herdr_dictation_trace', JSON.stringify(list.slice(-40)));
  } catch { /* storage unavailable */ }
}

/**
 * Starts dictating. `onText` receives the full committed transcript so far
 * plus the current interim fragment — the caller renders both in the input
 * and owns where they land (cursor position, existing draft).
 */
export function startDictation(
  language: string,
  textCallback: (finalText: string, interimText: string) => void,
  issueCallback?: (message: string) => void,
): boolean {
  const Ctor = recognitionCtor();
  if (!Ctor) {
    issueCallback?.('This browser has no voice dictation.');
    return false;
  }
  stopDictation();
  committed = '';
  sessionFinal = '';
  onText = textCallback;
  onIssue = issueCallback;
  wantListening = true;

  const wireRecognition = (target: SpeechRecognitionLike) => {
    target.lang = language || navigator.language || 'en-US';
    target.continuous = true;
    target.interimResults = true;

    target.onresult = (event) => {
      // Some Android recognizers emit every refinement as a cumulative final
      // result ("this", "this is", "this is just"…), so the list can't simply
      // be joined — each entry already contains the earlier ones. Drop any
      // result whose text is a proper prefix of a later result's text; what
      // remains is either one cumulative transcript or genuinely distinct
      // final segments.
      const kept: SpeechRecognitionResultLike[] = [];
      const seen: Record<string, true> = {};
      for (let i = 0; i < event.results.length; i++) {
        const text = event.results[i][0].transcript.trim();
        // Identical restatements (some recognizers re-fire the same final
        // result twice in one event) keep only the last copy.
        if (seen[text]) continue;
        seen[text] = true;
        let covered = false;
        for (let j = i + 1; j < event.results.length; j++) {
          const later = event.results[j][0].transcript.trim();
          if (later.length >= text.length && later.startsWith(text)) { covered = true; break; }
        }
        if (!covered) kept.push(event.results[i]);
      }
      let finals = '';
      let interim = '';
      for (const result of kept) {
        if (result.isFinal) finals += result[0].transcript;
        else interim += result[0].transcript;
      }
      // An interim that only restates already-finalized text adds nothing.
      const lastFinal = [...kept].reverse().find((result) => result.isFinal);
      if (lastFinal) {
        const finalText = lastFinal[0].transcript.trim();
        const interimText = interim.trim();
        if (interimText && finalText.startsWith(interimText)) interim = '';
      }
      // A restarted session may replay the text we already folded into
      // committed (the recognizer's list carries over despite the fresh
      // recognizer). If the new session transcript starts with committed,
      // it IS the whole transcript so far — don't join them.
      sessionFinal = finals;
      const trimmedSession = sessionFinal.trim();
      const joined = committed && trimmedSession.startsWith(committed)
        ? trimmedSession
        : committed && trimmedSession
          ? `${committed} ${trimmedSession}`
          : committed || trimmedSession;
      // Same replay guard for the interim: a fresh session's interim may
      // restate committed text before any final arrives.
      if (committed && interim.trim().startsWith(committed)) {
        interim = interim.trim().slice(committed.length).trimStart();
      }
      dictationTrace({ kind: 'result', committed, finals, interim, joined });
      onText?.(joined, interim);
    };

    target.onerror = (event) => {
      if (event.error === 'no-speech' || event.error === 'aborted') return;
      wantListening = false;
      dictationState.set('error');
      const message = event.error === 'not-allowed' || event.error === 'service-not-allowed'
        ? 'Microphone permission denied — allow it in the browser site settings.'
        : event.error === 'audio-capture'
          ? 'No microphone found on this device.'
          : event.error === 'network'
            ? 'Voice dictation needs a network connection on this device.'
            : 'Voice dictation failed.';
      onIssue?.(message);
    };

    // Android Chrome ends the session on a pause even with continuous=true;
    // restart while the user still wants dictation so pauses don't kill it.
    target.onend = () => {
      dictationTrace({ kind: 'end', sessionFinal, committed, wantListening });
      // Fold this session's transcript into committed before the restart so
      // the next session's fresh result list can't repeat it.
      const folded = sessionFinal.trim();
      if (folded) committed = committed ? `${committed} ${folded}` : folded;
      sessionFinal = '';
      if (!wantListening) {
        dictationState.set('off');
        return;
      }
      // A fresh recognizer per session: some Android builds keep the previous
      // session's cumulative result list alive across start() on the same
      // object, which would re-emit text we just folded into committed.
      const NextCtor = recognitionCtor();
      const next = NextCtor ? new NextCtor() : undefined;
      if (!next) {
        wantListening = false;
        dictationState.set('error');
        onIssue?.('Voice dictation stopped.');
        return;
      }
      recognition = next;
      wireRecognition(next);
      try {
        next.start();
      } catch {
        wantListening = false;
        dictationState.set('error');
        onIssue?.('Voice dictation stopped.');
      }
    };
  };

  recognition = new Ctor();
  wireRecognition(recognition);

  try {

    recognition.start();
    dictationState.set('listening');
    return true;
  } catch {
    wantListening = false;
    dictationState.set('error');
    onIssue?.('Voice dictation could not start.');
    return false;
  }
}

export function stopDictation(): void {
  wantListening = false;
  const active = recognition;
  recognition = undefined;
  if (active) {
    active.onresult = null;
    active.onerror = null;
    active.onend = null;
    try { active.stop(); } catch { /* already stopped */ }
  }
  dictationState.set('off');
}

export function dictationLanguage(): string {
  return get(speechLanguage);
}
