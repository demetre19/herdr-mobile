<script lang="ts">
  import { onDestroy } from 'svelte';
  import Button from '$components/ui/Button.svelte';
  import {
    classifyLocalhostContent,
    localhostFetch,
    localhostPortUp,
    type LocalhostContentKind,
  } from '$lib/localhost';
  import { localhostPorts, localhostEnabled, setLocalhostEnabled } from '$lib/preferences';
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
  let scanning = $state(true);
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
  const configuredPorts = $derived($localhostPorts);
  const upPorts = $derived(configuredPorts.filter((port) => portStates[port] === 'up'));
  const scanPending = $derived(configuredPorts.some((port) => portStates[port] === 'checking'));

  // Probe when the effective relay or the configured port list changes. The
  // signature guard keeps unrelated reactivity from re-scanning, and keeps
  // two effects from both firing probePorts on mount (the second would bump
  // the generation and discard the first's results).
  let probedSignature = $state('');
  $effect.pre(() => {
    const relay = effectiveRelayId;
    const signature = `${relay}|${configuredPorts.join(',')}`;
    if (!relay || signature === probedSignature) return;
    probedSignature = signature;
    selectedRelayId = relay;
    activePort = null;
    clearResponse();
    probePorts(relay);
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
    scanning = true;
    portStates = Object.fromEntries(configuredPorts.map((port) => [port, 'checking']));
    await Promise.all(configuredPorts.map(async (port) => {
      const up = await localhostPortUp(relay, port);
      if (generation !== probeGeneration) return;
      portStates = { ...portStates, [port]: up ? 'up' : 'down' };
    }));
    if (generation === probeGeneration) scanning = false;
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
  function refreshActive(): void {
    if (activePort === null || loading) return;
    void load(activePort, activePath);
  }
</script>

<div class="localhost-view">
  {#if !$localhostEnabled}
    <div class="localhost-empty">
      <p>Localhost is turned off in Settings.</p>
      <Button variant="ghost" onclick={() => setLocalhostEnabled(true)}>Turn on</Button>
    </div>
  {:else if connectedRelays.length === 0}
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

    <div class="localhost-ports" aria-label="Localhost ports">
      {#if scanPending}<span class="localhost-scan" role="status">Scanning…</span>{/if}
      {#each upPorts as port (port)}
        <button
          class="localhost-port-chip up"
          class:active={activePort === port}
          disabled={loading}
          aria-label={`Port ${port}, service available`}
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

    {#if !scanPending && !upPorts.length && activePort === null && !loading}
      <div class="localhost-empty">
        <p>No services answered on the configured ports. Start one on this computer, or edit the port list in Settings → Localhost Ports.</p>
      </div>
    {/if}

    {#if loading}
      <div class="localhost-status" role="status">Loading :{activePort}{activePath}…</div>
    {:else if errorMessage}
      <div class="localhost-status localhost-error" role="alert">{errorMessage}</div>
    {:else if activePort !== null && responseText}
      <div class="localhost-meta">
        <span class="localhost-meta-url">:{activePort}{activePath}</span>
        <Button class="localhost-reload" variant="ghost" size="icon" aria-label="Reload page" title="Reload page" onclick={refreshActive}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M21 12a9 9 0 1 1-2.64-6.36"></path><path d="M21 3v6h-6"></path>
          </svg>
        </Button>
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
        <p>Pick a port above to browse the service running on this computer.</p>
      </div>
    {/if}
  {/if}
</div>
