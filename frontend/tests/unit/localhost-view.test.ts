import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import LocalhostView from '$components/LocalhostView.svelte';
import { relayStore } from '$lib/store';
import type { RelayConfig } from '$lib/types';

function relayConfig(id: string, label: string): RelayConfig {
  return {
    id,
    label,
    url: '',
    token: '',
    transport: 'hybrid',
    gatewayUrl: 'wss://gw.example',
  } as unknown as RelayConfig;
}

function connectedStatus() {
  return { status: 'connected' } as never;
}

afterEach(() => {
  vi.restoreAllMocks();
  relayStore.relayConfigs.set([]);
  relayStore.connections.set(new Map());
});

describe('localhost viewer', () => {
  it('scans ports and renders an HTML response in a sandboxed iframe', async () => {
    relayStore.relayConfigs.set([relayConfig('relay-a', 'Mac mini')]);
    relayStore.connections.set(new Map([['relay-a', connectedStatus()]]));
    const sendCommand = vi.spyOn(relayStore, 'sendCommand').mockImplementation((relayId: string, payload: Record<string, unknown>) => {
      const port = Number(payload.port);
      if (port === 3000) {
        return Promise.resolve({
          ok: true,
          phase: 'completed',
          data: { status: 200, content_type: 'text/html; charset=utf-8', body: btoa('<html><body><h1>Dev server</h1></body></html>'), truncated: false },
        } as never);
      }
      return Promise.reject(new Error(`No service responded on port ${port}`));
    });

    render(LocalhostView, { relayId: 'relay-a' });

    // Port 3000 comes up, the rest stay down.
    await waitFor(() => expect(screen.getByRole('button', { name: /Port 3000, service available/ })).toBeTruthy());
    expect(screen.getByRole('button', { name: 'Port 3000, service available' }).className).toContain('up');
    expect(screen.getByRole('button', { name: 'Port 3001' }).className).toContain('down');

    await userEvent.click(screen.getByRole('button', { name: 'Port 3000, service available' }));
    await waitFor(() => expect(screen.getByTitle('Localhost page from port 3000')).toBeTruthy());
    const frame = screen.getByTitle('Localhost page from port 3000') as HTMLIFrameElement;
    expect(frame.getAttribute('sandbox')).toBe('');
    expect(frame.getAttribute('srcdoc')).toContain('<h1>Dev server</h1>');
    expect(sendCommand).toHaveBeenCalledWith('relay-a', { type: 'local_http_fetch', port: 3000, path: '/' }, 10_000);
  });

  it('shows the relay error when the fetch fails after a successful probe', async () => {
    relayStore.relayConfigs.set([relayConfig('relay-a', 'Mac mini')]);
    relayStore.connections.set(new Map([['relay-a', connectedStatus()]]));
    let calls = 0;
    vi.spyOn(relayStore, 'sendCommand').mockImplementation(() => {
      calls += 1;
      if (calls === 1) {
        // Port scan probe succeeds.
        return Promise.resolve({
          ok: true,
          phase: 'completed',
          data: { status: 200, content_type: 'text/html', body: btoa('<html></html>'), truncated: false },
        } as never);
      }
      // The tap's fetch fails (service died between probe and tap).
      return Promise.reject(new Error('No service responded on port 3000'));
    });

    render(LocalhostView, { relayId: 'relay-a' });
    await waitFor(() => expect((screen.getByRole('button', { name: 'Port 3000, service available' }) as HTMLButtonElement).disabled).toBe(false));
    await userEvent.click(screen.getByRole('button', { name: 'Port 3000, service available' }));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('No service responded on port 3000'));
  });

  it('renders an empty state when no relay is connected', () => {
    relayStore.relayConfigs.set([relayConfig('relay-a', 'Mac mini')]);
    relayStore.connections.set(new Map());
    render(LocalhostView, {});
    expect(screen.getByText(/No connected relay/)).toBeTruthy();
  });
});
