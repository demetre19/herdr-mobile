import { afterEach, describe, expect, it, vi } from 'vitest';
import { classifyLocalhostContent, localhostFetch, localhostPortUp, LOCALHOST_PORTS } from '$lib/localhost';
import { relayStore } from '$lib/store';

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

describe('localhost port range', () => {
  it('covers exactly 3000 through 3010', () => {
    expect(LOCALHOST_PORTS).toEqual([3000, 3001, 3002, 3003, 3004, 3005, 3006, 3007, 3008, 3009, 3010]);
  });
});
