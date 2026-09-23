<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import Button from '$components/ui/Button.svelte';
  import AppSelect from '$components/ui/AppSelect.svelte';
  import { agentOpeningView } from '$lib/agent-view';
  import Card from '$components/ui/Card.svelte';
  import { suggestedLaunchName } from '$lib/launch';
  import { defaultAgentView, defaultDirectories, paneAgentViewOverrides, setDefaultDirectory } from '$lib/preferences';
  import { targetRefForAgent } from '$lib/resource-id';
  import { replaceView } from '$lib/router';
  import { relayStore } from '$lib/store';

  let {
    relayId: requestedRelayId = '',
    workspaceId = '',
    cwd: requestedCwd = '',
    readOnlyRelayIds = new Set<string>(),
  }: {
    relayId?: string;
    workspaceId?: string;
    cwd?: string;
    readOnlyRelayIds?: Set<string>;
  } = $props();
  const relays = relayStore.relayConfigs;
  const connections = relayStore.connections;
  const workspaces = relayStore.workspaces;

  let relayId = $state('');
  let profileId = $state('');
  let cwd = $state('');
  let name = $state('');
  let prompt = $state('');
  let directoryOpen = $state(false);
  let status = $state('');
  let error = $state(false);
  let submitting = $state(false);
  let loadedRelay = '';
  // A deep link's target relay may connect after a faster sibling. Until the
  // reader picks a relay by hand, the requested one may still claim the form
  // when it becomes ready; without this the first-connected relay wins the
  // race and the link's workspace and directory are silently dropped.
  let requestedRelayPending = untrack(() => Boolean(requestedRelayId));
  let directoryLoadGeneration = 0;
  let directoryRelayId = $state('');
  let directoryBrowser: HTMLDivElement;
  let directoryQuery = $state('');
  let directoryNotice = $state('');

  const connectedRelays = $derived($relays.filter((relay) => {
    const connection = $connections.get(relay.id);
    return connection?.status === 'connected' && connection.inventory.state === 'ready';
  }));
  const unavailableRelays = $derived($relays.filter((relay) => {
    const connection = $connections.get(relay.id);
    return connection?.status === 'connected' && connection.inventory.state !== 'ready';
  }));
  const connection = $derived($connections.get(relayId));
  const filteredDirectories = $derived.by(() => {
    const entries = connection?.directoryBrowser?.directories || [];
    const query = directoryQuery.trim().toLowerCase();
    if (!query || query.includes('/')) return entries;
    return entries.filter((entry) => entry.name.toLowerCase().includes(query));
  });
  const profiles = $derived(connection?.agentProfiles || []);
  const targetWorkspace = $derived(
    $workspaces.find((workspace) => (
      workspace.relay_id === relayId && workspace.workspace_id === workspaceId
    )) || null,
  );
  const readOnly = $derived(readOnlyRelayIds.has(relayId));

  $effect(() => {
    if (requestedRelayPending && connectedRelays.some((relay) => relay.id === requestedRelayId)) {
      requestedRelayPending = false;
      relayId = requestedRelayId;
    }
    if (!connectedRelays.some((relay) => relay.id === relayId)) {
      relayId = connectedRelays.some((relay) => relay.id === requestedRelayId)
        ? requestedRelayId
        : connectedRelays[0]?.id || '';
    }
    if (!profiles.some((profile) => profile.id === profileId)) profileId = profiles[0]?.id || '';
    if (relayId && relayId !== loadedRelay) {
      loadedRelay = relayId;
      directoryRelayId = '';
      const initialPath = relayId === requestedRelayId
        ? (requestedCwd || $defaultDirectories[relayId] || '')
        : ($defaultDirectories[relayId] || '');
      cwd = initialPath;
      if (initialPath) {
        directoryRelayId = relayId;
        name = suggestedLaunchName(initialPath, profileId);
      }
      void loadDirectory(initialPath);
    }
  });

  const isDefaultDirectory = $derived(Boolean(cwd) && expandHome($defaultDirectories[relayId] || '') === cwd);

  async function loadDirectory(rawPath: string) {
    const path = expandHome(rawPath);
    const loadRelayId = relayId;
    const loadConnection = connection;
    const generation = ++directoryLoadGeneration;
    if (!loadRelayId || !loadConnection?.capabilities.includes('directory_browser')) return;
    directoryQuery = '';
    directoryNotice = '';
    try {
      const listing = await relayStore.listDirectories(loadRelayId, path);
      if (generation !== directoryLoadGeneration || relayId !== loadRelayId) return;
      directoryOpen = false;
      cwd = listing.current.path;
      directoryRelayId = loadRelayId;
      name = suggestedLaunchName(cwd, profileId);
    } catch {
      // The store exposes the relay error next to the directory browser.
    }
  }

  function expandHome(path: string): string {
    const home = connection?.home || '';
    if (!home) return path;
    if (path === '~') return home;
    if (path.startsWith('~/')) return home + path.slice(1);
    return path;
  }

  async function submitDirectoryQuery() {
    const query = directoryQuery.trim();
    // Plain words filter the listing; anything path-like navigates.
    if (!query || (query !== '~' && !query.includes('/'))) return;
    const target = expandHome(query);
    await loadDirectory(target);
    const landed = connection?.directoryBrowser?.current.path;
    directoryNotice = landed && landed !== target
      ? `That path isn't reachable — showing ${connection?.directoryBrowser?.current.label || landed} instead.`
      : '';
  }

  function updateName() {
    name = suggestedLaunchName(cwd, profileId);
  }

  function closeDirectoryForOtherField(event: FocusEvent) {
    if (event.target instanceof Node && !directoryBrowser.contains(event.target)) directoryOpen = false;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!relayId || readOnly || connection?.directoryLoading || directoryRelayId !== relayId || !profileId || !cwd || !name) return;
    submitting = true;
    error = false;
    status = 'Starting agent…';
    try {
      const launchName = name.trim();
      const launchCwd = cwd.trim();
      const result = await relayStore.sendCommand(relayId, {
        type: 'agent_start',
        profile_id: profileId,
        name: launchName,
        cwd: launchCwd,
        prompt,
        workspace_id: targetWorkspace?.workspace_id || '',
      }, 45_000);
      const warning = String(result.data?.warning || '');
      status = warning || 'Agent started.';
      error = Boolean(warning);
      prompt = '';
      name = '';
      relayStore.showToast(status, error);
      const rawPaneId = String(result.data?.pane_id || '');
      const launchedAgent = await relayStore.waitForAgent(relayId, {
        rawPaneId,
        name: launchName,
        cwd: launchCwd,
      });
      const target = launchedAgent ? targetRefForAgent(launchedAgent) : null;
      replaceView(launchedAgent && target
        ? agentOpeningView(
          launchedAgent,
          get(connections).get(launchedAgent.relay_id),
          get(defaultAgentView),
          get(paneAgentViewOverrides),
        )
        : { view: 'agents' });
    } catch (caught) {
      status = (caught as Error).message;
      error = true;
      relayStore.showToast(status, true);
    } finally {
      submitting = false;
    }
  }
</script>

<main class="page launch-page" aria-labelledby="launch-title">
  <h2 id="launch-title">Start Agent</h2>
  <Card>
    <form class="form-stack" onfocusin={closeDirectoryForOtherField} onsubmit={submit}>
      <label for="launch-relay">Computer</label>
      <AppSelect
        id="launch-relay"
        options={connectedRelays.map((relay) => ({ value: relay.id, label: relay.label }))}
        bind:value={relayId}
        placeholder={connectedRelays.length ? 'Select a computer' : 'No ready relays'}
        aria-label="Computer"
        onchange={() => { requestedRelayPending = false; }}
      />
      {#if unavailableRelays.length}
        <p class="warning" role="status">Agent inventory is unavailable on {unavailableRelays.map((relay) => relay.label).join(', ')}.</p>
      {/if}
      {#if readOnly}<p class="warning" role="status">This paired device has read-only access to the selected relay.</p>{/if}
      {#if targetWorkspace}
        <p class="hint">New tab in workspace <strong>{targetWorkspace.label}</strong>. The desktop keeps its current focus.</p>
      {/if}

      <label for="launch-profile">Agent</label>
      <AppSelect
        id="launch-profile"
        options={profiles.map((profile) => ({ value: profile.id, label: profile.label || profile.id }))}
        bind:value={profileId}
        placeholder={profiles.length ? 'Select an agent' : 'No agent profiles available'}
        aria-label="Agent"
        onchange={updateName}
      />

      <span id="launch-cwd-label" class="field-label">Working Directory</span>
      <div bind:this={directoryBrowser} class:open={directoryOpen} class="directory-browser" aria-labelledby="launch-cwd-label">
        <div class="directory-toolbar">
          <Button
            size="icon"
            variant="secondary"
            aria-label="Open parent directory"
            disabled={!connection?.directoryBrowser?.parent}
            onclick={() => connection?.directoryBrowser?.parent && loadDirectory(connection.directoryBrowser.parent)}
          >↑</Button>
          <button
            class="directory-current"
            type="button"
            aria-expanded={directoryOpen}
            aria-controls="launch-directory-list"
            onclick={() => { directoryOpen = !directoryOpen; }}
          >
            <span>{connection?.directoryBrowser?.current.label || cwd || (connection?.directoryLoading ? 'Loading…' : 'Unavailable')}</span>
            <span aria-hidden="true">⌄</span>
          </button>
          <button
            type="button"
            class="directory-default-pin"
            class:pinned={isDefaultDirectory}
            aria-pressed={isDefaultDirectory}
            aria-label={isDefaultDirectory ? 'Remove default folder' : 'Set as default folder'}
            title={isDefaultDirectory ? 'This folder is the default for new agents — tap to clear' : 'Start new agents in this folder by default'}
            onclick={() => { setDefaultDirectory(relayId, isDefaultDirectory ? '' : cwd); }}
          >
            <svg viewBox="0 0 24 24" fill={isDefaultDirectory ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M12 17v5M9 4h6l1 7 3 3H5l3-3z"></path></svg>
          </button>
        </div>
        {#if directoryOpen}
          <div id="launch-directory-list" class="directory-list" aria-label="Subdirectories">
            {#if !connection?.capabilities.includes('directory_browser')}
              <p>Update and restart this computer’s relay to browse directories.</p>
            {:else if connection.directoryLoading}
              <p>Loading folders…</p>
            {:else if connection.directoryError}
              <p role="alert">{connection.directoryError}</p>
            {:else}
              <input
                class="directory-search"
                type="search"
                bind:value={directoryQuery}
                onkeydown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void submitDirectoryQuery(); } }}
                placeholder="Filter or type a path…"
                aria-label="Filter directories or enter a path"
                autocomplete="off"
                autocapitalize="none"
                spellcheck="false"
              />
              {#if directoryNotice}<p class="directory-notice" role="status">{directoryNotice}</p>{/if}
              {#if connection?.directoryBrowser?.parent}
                <button type="button" onclick={() => loadDirectory(connection.directoryBrowser?.parent || '')}>↰ Parent folder</button>
              {/if}
              {#each filteredDirectories as directory (directory.path)}
                <button type="button" onclick={() => loadDirectory(directory.path)}>📁 {directory.name}</button>
              {/each}
              {#if connection?.directoryBrowser && !filteredDirectories.length}
                <p>{directoryQuery.trim() ? 'No folders match.' : 'This folder has no subdirectories. It remains selected.'}</p>
              {/if}
            {/if}
          </div>
        {/if}
      </div>
      <p class="hint">The folder shown above is selected. Tap it to browse; use ↑ or Parent folder to go back.</p>

      <label for="launch-name">Name</label>
      <input id="launch-name" bind:value={name} required maxlength="32" pattern={'[a-z][a-z0-9_-]{0,31}'} title="Start with a lowercase letter; use lowercase letters, numbers, underscores, or dashes." placeholder="project-codex" autocomplete="off" />

      <label for="launch-prompt">Initial task <span class="optional">(optional)</span></label>
      <textarea id="launch-prompt" bind:value={prompt} maxlength="100000" placeholder="Describe the task to start…"></textarea>
      <p class="hint">Sent to the agent as its first prompt after it starts.</p>
      <Button type="submit" disabled={submitting || readOnly || connection?.directoryLoading || directoryRelayId !== relayId || !relayId || !profileId || !cwd || !name}>Start Agent</Button>
      {#if status}<p class:error class="form-status" role="status">{status}</p>{/if}
    </form>
  </Card>
</main>
