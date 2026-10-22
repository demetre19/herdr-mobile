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
      relay: { id: 'harness', label: 'Harness', url: '', token: '' },
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
      update: { state: 'current', current_version: '', current_revision: '', available_version: '', available_revision: '', target_revision: '', upstream_version: '', upstream_revision: '', checked_at: 0, can_install: false, mode: '', reason: '', error: '' },
      appDeploy: { configured: false, origin: '', project: '', branch: '', revision: '', reason: '', state: 'idle', target_version: '', target_revision: '', checked_at: 0, error: '' },
      inventory: { state: 'ready', errorCode: '', message: '', lastAttemptAt: 0, lastSuccessAt: 0, stale: false },
      herdrStatus: { installed_client_version: '', server_version: '', server_protocol: 0, server_protocol_known: false, endpoint_protocol_generation: null, generation: 0, features: {} },
      authRejected: false,
      pairingRequired: false,
      pairingDeferred: false,
      speechLanguages: [],
      speechVoices: [],
      speechCacheDir: '',
      speechEngineInstalled: false,
      agentProfiles: [],
      directoryBrowser: null,
      directoryLoading: false,
      directoryError: '',
      pushStatus: '',
      vapidPublicKey: '',
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
