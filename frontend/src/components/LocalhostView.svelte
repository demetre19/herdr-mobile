<script lang="ts">
  import { onDestroy } from 'svelte';
  import Button from '$components/ui/Button.svelte';
  import {
    LOCALHOST_PORTS,
    classifyLocalhostContent,
    localhostFetch,
    localhostPortUp,
    type LocalhostContentKind,
  } from '$lib/localhost';
  import { relayStore } from '$lib/store';

  interface RelayOption {
    id: string;
    label: string;
    connected: boolean;
  }

  let { relayId = '' }: { relayId?: string } = $props();

  const relayConfigs = relayStore.relayConfigs;
  const connections = relayStore.connections;

  const relayOptions = $derived.by(() => {
    const configs = $relayConfigs;
    const statuses = $connections;
    return configs.map((relay) => ({
      id: relay.id,
      label: relay.label || relay.id,
      connected: statuses.get(relay.id)?.status === 'connected',
    }));
  });
  const connectedRelays = $derived(relayOptions.filter((relay) => relay.connected));

  type PortState = 'checking' | 'up' | 'down';
  let selectedRelayId = $state('');
  let portStates = $state<Record<number, PortState>>({});
  let activePort = $state<number | null>(null);
  let activePath = $state('/');
  let loading = $state(false);
  let errorMessage = $state('');
  let responseStatus = $state(0);
  let responseKind = $state<LocalhostContentKind>('text');
  let responseText = $state('');
  let responseTruncated = $state(false);
  let probeGeneration = 0;

  const effectiveRelayId = $derived(
    relayId || selectedRelayId || connectedRelays[0]?.id || '',
  );

  $effect.pre(() => {
    const nextRelay = effectiveRelayId;
    if (nextRelay && nextRelay !== selectedRelayId) {
      selectedRelayId = nextRelay;
      activePort = null;
      clearResponse();
      probePorts(nextRelay);
    }
  });

  function clearResponse(): void {
    errorMessage = '';
    responseStatus = 0;
    responseKind = 'text';
    responseText = '';
    responseTruncated = false;
  }

  async function probePorts(relay: string): Promise<void> {
    const generation = ++probeGeneration;
    portStates = Object.fromEntries(LOCALHOST_PORTS.map((port) => [port, 'checking']));
    await Promise.all(LOCALHOST_PORTS.map(async (port) => {
      const up = await localhostPortUp(relay, port);
      if (generation !== probeGeneration) return;
      portStates = { ...portStates, [port]: up ? 'up' : 'down' };
    }));
  }

  async function load(port: number, path: string): Promise<void> {
    if (!effectiveRelayId || loading) return;
    loading = true;
    errorMessage = '';
    activePort = port;
    activePath = path;
    responseStatus = 0;
    responseKind = 'text';
    responseText = '';
    responseTruncated = false;
    try {
      const result = await localhostFetch(effectiveRelayId, port, path);
      responseStatus = result.status;
      responseKind = classifyLocalhostContent(result.contentType);
      responseText = result.text;
      responseTruncated = result.truncated;
    } catch (error) {
      errorMessage = error instanceof Error ? error.message : 'Localhost request failed';
      activePort = null;
    } finally {
      loading = false;
    }
  }

  function prettyJson(text: string): string {
    try {
      return JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      return text;
    }
  }

  onDestroy(() => { probeGeneration += 1; });
</script>

<div class="localhost-view">
  {#if connectedRelays.length === 0}
    <div class="localhost-empty">
      <p>No connected relay. Localhost browsing needs a connected computer.</p>
      <Button variant="ghost" onclick={() => probePorts(effectiveRelayId)}>Retry</Button>
    </div>
  {:else}
    {#if connectedRelays.length > 1}
      <div class="localhost-relays" role="tablist" aria-label="Computer">
        {#each connectedRelays as relay (relay.id)}
          <button
            class="localhost-relay-chip"
            class:active={relay.id === effectiveRelayId}
            onclick={() => { selectedRelayId = relay.id; }}
          >{relay.label}</button>
        {/each}
      </div>
    {/if}

    <div class="localhost-ports" aria-label="Localhost ports 3000 to 3010">
      {#each LOCALHOST_PORTS as port (port)}
        <button
          class="localhost-port-chip"
          class:up={portStates[port] === 'up'}
          class:down={portStates[port] === 'down'}
          class:active={activePort === port}
          disabled={portStates[port] !== 'up' || loading}
          aria-label={`Port ${port}${portStates[port] === 'up' ? ', service available' : ''}`}
          onclick={() => load(port, activePort === port ? activePath : '/')}
        >
          <span class="localhost-port-dot" aria-hidden="true"></span>:{port}
        </button>
      {/each}
      <Button class="localhost-refresh" variant="ghost" size="icon" aria-label="Rescan ports" onclick={() => probePorts(effectiveRelayId)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M21 12a9 9 0 1 1-2.64-6.36"></path><path d="M21 3v6h-6"></path>
        </svg>
      </Button>
    </div>

    {#if loading}
      <div class="localhost-status" role="status">Loading :{activePort}{activePath}…</div>
    {:else if errorMessage}
      <div class="localhost-status localhost-error" role="alert">{errorMessage}</div>
    {:else if activePort !== null && responseText}
      <div class="localhost-meta">
        <span>:{activePort}{activePath}</span>
        <span>{responseStatus}{responseTruncated ? ' · truncated' : ''}</span>
      </div>
      {#if responseKind === 'html'}
        <iframe
          class="localhost-frame"
          title="Localhost page from port {activePort}"
          sandbox=""
          srcdoc={responseText}
        ></iframe>
      {:else if responseKind === 'json'}
        <pre class="localhost-text">{prettyJson(responseText)}</pre>
      {:else}
        <pre class="localhost-text">{responseText}</pre>
      {/if}
    {:else if activePort !== null}
      <div class="localhost-status">:{activePort} returned an empty response</div>
    {:else}
      <div class="localhost-empty">
        <p>Pick a port with a green dot to browse the service running on this computer.</p>
      </div>
    {/if}
  {/if}
</div>
