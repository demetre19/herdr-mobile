import { render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import LocalhostView from '$components/LocalhostView.svelte';
import AgentList from '$components/AgentList.svelte';
import { DEFAULT_LOCALHOST_PORTS } from '$lib/config';
import { localhostPorts, localhostEnabled, setLocalhostEnabled } from '$lib/preferences';
import { localhostLivePorts } from '$lib/localhost';
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
  localhostPorts.set([...DEFAULT_LOCALHOST_PORTS]);
  localhostEnabled.set(true);
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

    // Only the live port renders a chip; dead ports never appear.
    await waitFor(() => expect(screen.getByRole('button', { name: 'Port 3000, service available' })).toBeTruthy());
    expect(screen.queryByRole('button', { name: 'Port 3001, service available' })).toBeNull();
    expect(screen.queryByText(/Scanning/)).toBeNull();

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

  it('scans only the configured port list', async () => {
    relayStore.relayConfigs.set([relayConfig('relay-a', 'Mac mini')]);
    relayStore.connections.set(new Map([['relay-a', connectedStatus()]]));
    localhostPorts.set([4000, 8080]);
    const probed: number[] = [];
    vi.spyOn(relayStore, 'sendCommand').mockImplementation((_relayId: string, payload: Record<string, unknown>) => {
      probed.push(Number(payload.port));
      return Promise.reject(new Error(`No service responded on port ${payload.port}`));
    });

    render(LocalhostView, { relayId: 'relay-a' });
    await waitFor(() => expect(screen.getByText(/No services answered/)).toBeTruthy());
    expect(probed.sort((a, b) => a - b)).toEqual([4000, 8080]);
    expect(screen.queryByRole('button', { name: /Port 3000/ })).toBeNull();
  });

  it('shows a live port outside the default range', async () => {
    relayStore.relayConfigs.set([relayConfig('relay-a', 'Mac mini')]);
    relayStore.connections.set(new Map([['relay-a', connectedStatus()]]));
    localhostPorts.set([5173]);
    vi.spyOn(relayStore, 'sendCommand').mockResolvedValue({
      ok: true,
      phase: 'completed',
      data: { status: 200, content_type: 'text/html', body: btoa('<html>vite</html>'), truncated: false },
    } as never);

    render(LocalhostView, { relayId: 'relay-a' });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Port 5173, service available' })).toBeTruthy());
    await userEvent.click(screen.getByRole('button', { name: 'Port 5173, service available' }));
    await waitFor(() => expect(screen.getByTitle('Localhost page from port 5173')).toBeTruthy());
  });

  it('shows the disabled state and can re-enable from the viewer', async () => {
    setLocalhostEnabled(false);
    render(LocalhostView, { relayId: 'relay-a' });
    expect(screen.getByText(/Localhost is turned off in Settings/)).toBeTruthy();
    await userEvent.click(screen.getByRole('button', { name: 'Turn on' }));
    expect(screen.queryByText(/Localhost is turned off/)).toBeNull();
  });
});

describe('localhost cards in the agent list', () => {
  function renderAgentList() {
    return render(AgentList, {
      agents: [],
      relays: [relayConfig('relay-a', 'Mac mini')],
      connections: new Map([['relay-a', { status: 'connected', inventory: { state: 'ready' } } as never]]),
      responding: new Set<string>(),
      onopen: vi.fn(),
    });
  }

  it('renders a card only for relays with live ports, and hides everything when toggled off', async () => {
    vi.spyOn(relayStore, 'sendCommand').mockRejectedValue(new Error('No service responded on port 3000'));
    localhostLivePorts.set(new Map([['relay-a', [3000, 5173]]]));

    const { unmount } = renderAgentList();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Browse localhost services on Mac mini' })).toBeTruthy());
    expect(screen.getByText(/Live on 3000, 5173 · Mac mini/)).toBeTruthy();
    unmount();

    // No live ports → no card, even with the toggle on.
    localhostLivePorts.set(new Map());
    const empty = renderAgentList();
    await waitFor(() => expect(screen.queryByRole('button', { name: /Browse localhost services/ })).toBeNull());
    empty.unmount();

    // Toggle off → no card even with live ports.
    localhostLivePorts.set(new Map([['relay-a', [3000]]]));
    localhostEnabled.set(false);
    const off = renderAgentList();
    await waitFor(() => expect(screen.queryByRole('button', { name: /Browse localhost services/ })).toBeNull());
    off.unmount();
  });
});
