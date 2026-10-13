import { afterEach, describe, expect, it, vi } from 'vitest';
import { classifyLocalhostContent, localhostFetch, localhostLivePorts, localhostPortUp, LOCALHOST_PORTS, refreshLocalhostAvailability } from '$lib/localhost';
import { parseLocalhostPorts } from '$lib/config';
import { localhostPorts } from '$lib/preferences';
import { relayStore } from '$lib/store';
import { get } from 'svelte/store';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('localhost content classification', () => {
  it('classifies by mime type with parameters stripped', () => {
    expect(classifyLocalhostContent('text/html; charset=utf-8')).toBe('html');
    expect(classifyLocalhostContent('application/xhtml+xml')).toBe('html');
    expect(classifyLocalhostContent('application/json')).toBe('json');
    expect(classifyLocalhostContent('application/vnd.api+json')).toBe('json');
    expect(classifyLocalhostContent('text/plain')).toBe('text');
    expect(classifyLocalhostContent('application/xml')).toBe('text');
    expect(classifyLocalhostContent('image/png')).toBe('binary');
    expect(classifyLocalhostContent('')).toBe('binary');
  });
});

describe('localhost fetch', () => {
  it('decodes the base64 body and surfaces status and truncation', async () => {
    const body = btoa('{"hello":"localhost"}');
    const sendCommand = vi.spyOn(relayStore, 'sendCommand').mockResolvedValue({
      ok: true,
      phase: 'completed',
      data: { status: 200, content_type: 'application/json', body, truncated: false },
    } as never);
    const result = await localhostFetch('relay-a', 3000, '/api/status');
    expect(sendCommand).toHaveBeenCalledWith('relay-a', { type: 'local_http_fetch', port: 3000, path: '/api/status' }, 10_000);
    expect(result.status).toBe(200);
    expect(result.contentType).toBe('application/json');
    expect(result.text).toBe('{"hello":"localhost"}');
    expect(result.truncated).toBe(false);
  });

  it('rejects an invalid relay payload', async () => {
    vi.spyOn(relayStore, 'sendCommand').mockResolvedValue({
      ok: true,
      phase: 'completed',
      data: { status: 200 },
    } as never);
    await expect(localhostFetch('relay-a', 3000, '/')).rejects.toThrow('invalid localhost response');
  });

  it('propagates command failures as rejections', async () => {
    vi.spyOn(relayStore, 'sendCommand').mockRejectedValue(new Error('No service responded on port 3000'));
    await expect(localhostFetch('relay-a', 3000, '/')).rejects.toThrow('No service responded');
  });
});

describe('localhost port probe', () => {
  it('resolves true only when the fetch succeeds', async () => {
    vi.spyOn(relayStore, 'sendCommand').mockResolvedValue({
      ok: true,
      phase: 'completed',
      data: { status: 200, content_type: 'text/html', body: btoa('<html></html>'), truncated: false },
    } as never);
    await expect(localhostPortUp('relay-a', 3000)).resolves.toBe(true);

    vi.spyOn(relayStore, 'sendCommand').mockRejectedValue(new Error('No service responded on port 3001'));
    await expect(localhostPortUp('relay-a', 3001)).resolves.toBe(false);
  });
});

describe('localhost availability cache', () => {
  it('records live ports per relay and dedupes in-flight scans', async () => {
    localhostPorts.set([3000, 3001, 8080]);
    let calls = 0;
    vi.spyOn(relayStore, 'sendCommand').mockImplementation((_relayId: string, payload: Record<string, unknown>) => {
      calls += 1;
      const port = Number(payload.port);
      if (port === 3000 || port === 8080) {
        return Promise.resolve({ ok: true, phase: 'completed', data: { status: 200, content_type: 'text/html', body: btoa('<html></html>'), truncated: false } } as never);
      }
      return Promise.reject(new Error(`No service responded on port ${port}`));
    });

    refreshLocalhostAvailability('relay-a', true);
    refreshLocalhostAvailability('relay-a', true); // in-flight dedupe
    await vi.waitFor(() => {
      expect(get(localhostLivePorts).get('relay-a')).toEqual([3000, 8080]);
    });
    const callsAfterFirstScan = calls;
    refreshLocalhostAvailability('relay-a'); // TTL guard is synchronous: no rescan
    expect(calls).toBe(callsAfterFirstScan);
  });
});

describe('localhost port range', () => {
  it('defaults to exactly 3000 through 3010', () => {
    expect(LOCALHOST_PORTS).toEqual([3000, 3001, 3002, 3003, 3004, 3005, 3006, 3007, 3008, 3009, 3010]);
  });
  it('parses the stored port list: dedupe, sort, validate, default fallback', () => {
    expect(parseLocalhostPorts(null)).toEqual(LOCALHOST_PORTS);
    expect(parseLocalhostPorts(undefined)).toEqual(LOCALHOST_PORTS);
    expect(parseLocalhostPorts('')).toEqual(LOCALHOST_PORTS);
    expect(parseLocalhostPorts('abc')).toEqual(LOCALHOST_PORTS);
    expect(parseLocalhostPorts('8080, 3000,3000 5173')).toEqual([3000, 5173, 8080]);
    expect(parseLocalhostPorts('0, 65536, -1, 4000')).toEqual([4000]);
    expect(parseLocalhostPorts(Array.from({ length: 40 }, (_, i) => 10000 + i).join(','))).toHaveLength(32);
  });
});
