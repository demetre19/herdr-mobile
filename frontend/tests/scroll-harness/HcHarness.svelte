<script lang="ts">
  import '../../src/app.css';
  import HcView from '$components/HcView.svelte';
  import { relayStore } from '$lib/store';
  import type { Agent, HcInventory, RelayConfig, RelayConnectionView } from '$lib/types';

  // Two "relays": LAPTOP (local) whose inventory includes one local space and
  // one federated remote space (@macbookpro-codex), and a second DESKTOP relay
  // with its own space — exercises both the machine-group boundary and the
  // per-space pin buttons.
  const relays: RelayConfig[] = [
    { id: 'laptop', label: 'LAPTOP', url: '', token: '' },
    { id: 'desktop', label: 'DESKTOP', url: '', token: '' },
  ];

  function agent(relayId: string, terminalId: string, name: string): Agent {
    return {
      relay_id: relayId,
      relay_label: relays.find((r) => r.id === relayId)?.label || relayId,
      raw_pane_id: terminalId,
      pane_id: `${relayId}:${terminalId}`,
      terminal_id: terminalId,
      server_session_id: 's1',
      generation: 1,
      agent_session_id: 'sess',
      agent: 'omp',
      name,
      status: 'working',
      cwd: '/tmp/proj',
      workspace_id: '',
    } as Agent;
  }

  const laptopInv: HcInventory = {
    state: 'ready',
    server_id: 'laptop',
    machines: ['macbookpro-codex'],
    spaces: [
      {
        id: 'sp-local',
        name: 'herdr-mobile',
        folder: '/tmp/hm',
        tabs: [
          { id: 'tab1', name: 'build', terminals: [{ terminal: 't11', pane: 't11' }] as never },
        ],
      },
      {
        id: 'sp-remote',
        name: 'of-machinery',
        folder: '/tmp/ofm',
        machine: 'macbookpro-codex',
        tabs: [
          { id: 'tab2', name: 'of-machinery', terminals: [{ terminal: 'macbookpro-codex/t147', pane: 'macbookpro-codex/t147' }] as never },
        ],
      },
      {
        id: 'hc-core:t99',
        tabs: [],
      },
    ],
    agents: [
      agent('laptop', 't11', 'build'),
      agent('laptop', 'macbookpro-codex/t147', 'of-machinery'),
      agent('laptop', 't99', 'core'),
    ],
  };

  // DESKTOP mirrors the real deployment: its window also mounts the remote
  // machine's space, so its inventory carries a qualified
  // `macbookpro-codex/t147` terminal plus a `machine`-tagged space. The
  // federation dedupe hides that card in favour of LAPTOP's — leaving the
  // remote space with zero visible rows. A machine heading must NOT render
  // for it (the stray @MACBOOKPRO-CODEX markers the phone showed).
  const desktopInv: HcInventory = {
    state: 'ready',
    server_id: 'desktop',
    machines: ['macbookpro-codex'],
    spaces: [
      {
        id: 'sp-desk',
        name: 'dents-direct',
        folder: '/tmp/dd',
        tabs: [
          { id: 'tab3', name: 'api', terminals: [{ terminal: 't2', pane: 't2' }] as never },
        ],
      },
      {
        id: 'sp-desk-remote',
        name: 'of-machinery',
        folder: '/tmp/ofm',
        machine: 'macbookpro-codex',
        tabs: [
          { id: 'tab4', name: 'of-machinery', terminals: [{ terminal: 'macbookpro-codex/t147', pane: 'macbookpro-codex/t147' }] as never },
        ],
      },
    ],
    agents: [agent('desktop', 't2', 'api'), agent('desktop', 'macbookpro-codex/t147', 'of-machinery')],
  };

  relayStore.hcInventories.set(new Map([
    ['laptop', laptopInv],
    ['desktop', desktopInv],
  ]));

  const conn: RelayConnectionView = {
    relay: { id: 'x', label: 'x', url: '', token: '' },
    status: 'connected',
    path: 'gateway',
    activeGatewayUrl: '',
    host: 'h',
    home: '/tmp',
    protocol: 1,
    version: '1.0.48',
    releaseVersion: '1.0.48',
    revision: 'x',
    update: { state: 'current', current_version: '', current_revision: '', available_version: '', available_revision: '', target_revision: '', upstream_version: '', upstream_revision: '', checked_at: 0, can_install: false, mode: '', reason: '', error: '' },
    appDeploy: { configured: false, origin: '', project: '', branch: '', revision: '', reason: '', state: 'idle', target_version: '', target_revision: '', checked_at: 0, error: '' },
    inventory: { state: 'ready', errorCode: '', message: '', lastAttemptAt: 0, lastSuccessAt: 0, stale: false },
    capabilities: [],
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
  } as RelayConnectionView;

  const connections = new Map<string, RelayConnectionView>([
    ['laptop', { ...conn, relay: relays[0], home: '/Users/apple' }],
    ['desktop', { ...conn, relay: relays[1], home: '/Users/apple' }],
  ]);

  let disclosure = $state<Record<string, boolean>>({});
</script>

<HcView {connections} {relays} bind:disclosure onopen={() => {}} />
