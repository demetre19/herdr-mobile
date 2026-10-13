/**
 * Localhost browsing: render web UIs served on a relay machine's loopback
 * interface. The relay enforces the security envelope (literal 127.0.0.1
 * host, restricted port range, GET only, bounded body); this module is the
 * phone-side command wrapper and content classification.
 */

import { relayStore } from './store';
import { localhostPorts } from './preferences';
import { DEFAULT_LOCALHOST_PORTS } from './config';

export const LOCALHOST_PORTS: number[] = DEFAULT_LOCALHOST_PORTS;

export type LocalhostContentKind = 'html' | 'json' | 'text' | 'binary';

export interface LocalhostFetchResult {
  status: number;
  contentType: string;
  text: string;
  truncated: boolean;
}

export function classifyLocalhostContent(contentType: string): LocalhostContentKind {
  const mime = String(contentType || '').split(';')[0].trim().toLowerCase();
  if (mime === 'text/html' || mime === 'application/xhtml+xml') return 'html';
  if (mime === 'application/json' || mime.endsWith('+json')) return 'json';
  if (mime.startsWith('text/') || mime === 'application/xml') return 'text';
  return 'binary';
}

function decodeBase64(value: string): Uint8Array {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

export function localhostFetch(
  relayId: string,
  port: number,
  path: string,
  timeoutMs = 10_000,
): Promise<LocalhostFetchResult> {
  const result = relayStore.sendCommand(relayId, { type: 'local_http_fetch', port, path }, timeoutMs);
  return result.then((command) => {
    const data = command.data as Record<string, unknown> | undefined;
    if (!data || typeof data.status !== 'number' || typeof data.body !== 'string') {
      throw new Error('Relay returned an invalid localhost response.');
    }
    const contentType = String(data.content_type || '');
    const bytes = decodeBase64(data.body);
    return {
      status: data.status,
      contentType,
      text: new TextDecoder('utf-8', { fatal: false }).decode(bytes),
      truncated: data.truncated === true,
    };
  });
}

/** Quick liveness probe for the port picker; resolves false on any failure. */
export async function localhostPortUp(relayId: string, port: number): Promise<boolean> {
  try {
    await localhostFetch(relayId, port, '/', 3_000);
    return true;
  } catch {
    return false;
  }
}
