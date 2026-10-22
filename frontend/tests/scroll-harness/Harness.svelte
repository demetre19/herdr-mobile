<script lang="ts">
  import '../../src/app.css';
  import TerminalView from '$components/TerminalView.svelte';
  import { relayStore } from '$lib/store';
  import type { Agent, TerminalFrame } from '$lib/types';

  const agent: Agent = {
    relay_id: 'harness',
    relay_label: 'Harness',
    raw_pane_id: 'harness-pane',
    pane_id: 'harness-pane',
    agent: 'omp',
    name: 'scroll-harness',
    status: 'working',
    cwd: '/tmp/harness',
  };

  // Fake a connected relay with the lease capability so the component takes
  // the phone's real code path (cell measurement, --terminal-cell-width,
  // lease requests). leasePaneSize resolves immediately with the requested
  // size.
  relayStore.connections.set(new Map([
    ['harness', {
      relay: { id: 'harness' },
      status: 'connected',
      capabilities: ['pane_size_lease', 'pane_size_lease_rows'],
      path: 'gateway',
      activeGatewayUrl: '',
      host: 'harness',
      home: '/tmp',
      protocol: 1,
      version: '1.0.23',
      releaseVersion: '1.0.23',
      revision: 'harness',
      update: { state: 'current' },
      appDeploy: {},
      inventory: { state: 'ready' },
      herdrStatus: {},
      authRejected: false,
      pairingRequired: false,
      pairingDeferred: false,
      transport: null,
      reconnectTimer: null,
      healthTimer: null,
      updateRestartTimer: null,
      closed: false,
      connectingSince: 0,
      lastMessageAt: Date.now(),
      directoryGeneration: 0,
    }],
  ]));
  const originalLease = relayStore.leasePaneSize.bind(relayStore);
  relayStore.leasePaneSize = (target: Agent, columns: number, rows: number) => {
    (window as unknown as Record<string, unknown>).__leased = { columns, rows };
    return Promise.resolve({ columns, rows });
  };
  void originalLease;


  let frame = $state<TerminalFrame | undefined>(undefined);
  const responding = new Set<string>();

  // Driver API: push a full-snapshot frame (same shape the relay sends).
  (window as unknown as Record<string, unknown>).__setFrame = (
    content: string,
    format = 'ansi',
  ) => {
    frame = { paneId: agent.pane_id, content, format };
  };
  (window as unknown as Record<string, unknown>).__scroller = () =>
    document.querySelector<HTMLElement>('.term-content');
</script>

<TerminalView {agent} allAgents={[agent]} {frame} {responding} />
