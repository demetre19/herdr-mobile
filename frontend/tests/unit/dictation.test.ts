import { afterEach, describe, expect, it, vi } from 'vitest';

import { dictationState, startDictation, stopDictation } from '$lib/dictation';
import { get } from 'svelte/store';

interface FakeResult {
  isFinal: boolean;
  0: { transcript: string };
  length: number;
}

function result(transcript: string, isFinal: boolean): FakeResult {
  return { isFinal, 0: { transcript }, length: 1 };
}

class FakeRecognition {
  static instances: FakeRecognition[] = [];
  lang = '';
  continuous = false;
  interimResults = false;
  onresult: ((event: { results: FakeResult[] }) => void) | null = null;
  onerror: ((event: { error: string }) => void) | null = null;
  onend: (() => void) | null = null;
  started = 0;
  stopped = 0;

  constructor() {
    FakeRecognition.instances.push(this);
  }

  start() {
    this.started += 1;
  }

  stop() {
    this.stopped += 1;
    this.onend?.();
  }

  emit(results: FakeResult[]) {
    this.onresult?.({ results });
  }
}

function installFake() {
  FakeRecognition.instances = [];
  vi.stubGlobal('SpeechRecognition', FakeRecognition);
}

afterEach(() => {
  stopDictation();
  vi.unstubAllGlobals();
});

describe('dictation transcript assembly', () => {
  it('renders cumulative final refinements once, not concatenated', () => {
    installFake();
    const seen: string[] = [];
    expect(startDictation('en-US', (finalText, interim) => seen.push(finalText + interim))).toBe(true);
    const rec = FakeRecognition.instances[0];

    // Android-style: every refinement arrives as a cumulative final result.
    rec.emit([result('this', true)]);
    rec.emit([result('this', true), result('this is', true)]);
    rec.emit([result('this', true), result('this is', true), result('this is just me', true)]);

    expect(seen.at(-1)).toBe('this is just me');
  });

  it('keeps genuinely distinct final segments joined', () => {
    installFake();
    const seen: string[] = [];
    startDictation('en-US', (finalText) => seen.push(finalText));
    const rec = FakeRecognition.instances[0];

    // Desktop-style: separate final segments, none a prefix of a later one.
    rec.emit([result('hello there', true), result('general kenobi', true)]);
    expect(seen.at(-1)).toBe('hello theregeneral kenobi');
  });

  it('drops an interim that only restates finalized text', () => {
    installFake();
    const seen: string[] = [];
    startDictation('en-US', (finalText, interim) => seen.push(finalText + '|' + interim));
    const rec = FakeRecognition.instances[0];

    rec.emit([result('this is', true), result('this', false)]);
    expect(seen.at(-1)).toBe('this is|');
  });

  it('folds the session transcript into committed text across restarts without repeating it', () => {
    installFake();
    const seen: string[] = [];
    startDictation('en-US', (finalText) => seen.push(finalText));
    const first = FakeRecognition.instances[0];

    first.emit([result('first part', true)]);
    first.onend?.(); // session ended mid-run → restart on a fresh recognizer
    const second = FakeRecognition.instances[1];
    expect(second).toBeDefined();
    expect(second.started).toBe(1);

    // The new session reports only its own text; committed carries the rest.
    second.emit([result('second part', true)]);
    expect(seen.at(-1)).toBe('first part second part');
  });

  it('stops restarting once dictation is stopped', () => {
    installFake();
    startDictation('en-US', () => {});
    const rec = FakeRecognition.instances[0];
    stopDictation();
    expect(rec.stopped).toBe(1);
    expect(get(dictationState)).toBe('off');
    expect(FakeRecognition.instances).toHaveLength(1);
  });
});
