<script lang="ts">
  import { onMount } from 'svelte';
  import { relayStore } from '$lib/store';
  import {
    agentLastActiveAt,
    agentStatusGroup,
    agentStatusTone,
    approvalButtonTone,
    approvalOptions,
    approvalPromptPreview,
    displayName,
    questionInteraction,
    shortModelLabel,
    tabName,
    worktreeTailStart,
  } from '$lib/agents';
  import { pinnedConversations, pinnedHcSpaces, prunePinnedHcSpaces, togglePinnedConversation, togglePinnedHcSpace, persistHcDisclosure } from '$lib/preferences';
  import { homeRelativePath } from '$lib/workspaces';
  import AgentLogo from './AgentLogo.svelte';
  import AppDialog from './ui/AppDialog.svelte';
  import Button from './ui/Button.svelte';
  import type { Agent, HcInventory, HcSpace, HcTab, RelayConfig, RelayConnectionView } from '$lib/types';

  const {
    connections,
    relays,
    onopen,
    disclosure = $bindable<Record<string, boolean>>({}),
  }: {
    connections: Map<string, RelayConnectionView>;
    relays: RelayConfig[];
    onopen: (agent: Agent) => void;
    disclosure?: Record<string, boolean>;
  } = $props();

  // Closed space groups remember their state. Default is open — only a
  // deliberate close lands in the pref (a re-rendered open never writes).
  function rememberDisclosure(key: string, event: ToggleEvent) {
    const details = event.currentTarget;
    if (!(details instanceof HTMLDetailsElement)) return;
    if (details.open) delete disclosure[key];
    else disclosure[key] = false;
    persistHcDisclosure(disclosure);
  }
  function disclosureOpen(key: string): boolean {
    return disclosure[key] ?? true;
  }

  const hcInventories = relayStore.hcInventories;
  const responding = relayStore.responding;
  const pinned = $derived(new Set($pinnedConversations));
  const pinnedSpaces = $derived(new Set($pinnedHcSpaces));

  // Pinned conversations ride the same pref as the legacy list — a hc: pane
  // key never collides with a legacy pane id, so one store serves both.
  const pinnedAgents = $derived.by(() => {
    const out: Agent[] = [];
    for (const [, inventory] of $hcInventories) {
      for (const agent of inventory.agents || []) {
        if (pinned.has(agent.pane_id)) out.push(agent);
      }
    }
    return out;
  });

  let relativeNow = $state(Date.now());
  onMount(() => {
    const timer = setInterval(() => { relativeNow = Date.now(); }, 60_000);
    return () => clearInterval(timer);
  });
  function relativeTimestamp(timestamp: number): string {
    if (!timestamp) return '';
    const seconds = Math.max(0, Math.floor((relativeNow - timestamp) / 1_000));
    if (seconds < 60) return 'now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h`;
    const days = Math.floor(hours / 24);
    if (days < 30) return `${days}d`;
    const months = Math.floor(days / 30);
    if (months < 12) return `${months}mo`;
    return `${Math.floor(months / 12)}y`;
  }
  function relayPath(relayId: string, path: string): string {
    return homeRelativePath(path, connections.get(relayId)?.home || '');
  }
  function relativeAge(agent: Agent): string {
    return relativeTimestamp(agentLastActiveAt(agent));
  }

  // Every relay that publishes an hc snapshot gets a section; relays whose
  // Herdr App core never answered still render a muted card so the phone can
  // tell "no app" from "relay without the feature".
  const sections = $derived.by(() => {
    const out: { relayId: string; label: string; inventory: HcInventory; connected: boolean }[] = [];
    for (const [relayId, inventory] of $hcInventories) {
      const relay = relays.find((r) => r.id === relayId);
      out.push({
        relayId,
        label: relay?.label || inventory.server_id || relayId,
        inventory,
        connected: connections.get(relayId)?.status === 'connected',
      });
    }
    out.sort((a, b) => a.label.localeCompare(b.label));
    return out;
  });

  type HcSpaceRow = { tab: HcTab; agent: Agent };
  type HcSpaceGroup = {
    key: string;
    space: HcSpace;
    relayId: string;
    rows: HcSpaceRow[];
  };

  // Every window space across all sections, in pin-array order for the
  // pinned subset. Space pins live under `hc:<relayId>:<space.id>` — the
  // id is the daemon's stable space id, so the pin survives inventory
  // rebuilds and relay reconnects.
  const spaceGroups = $derived.by((): HcSpaceGroup[] => {
    const out: HcSpaceGroup[] = [];
    for (const section of sections) {
      if (section.inventory.state === 'error') continue;
      const byAddress = agentsByAddress(section.inventory.agents);
      for (const space of (section.inventory.spaces || []).filter((s) => !isCoreSpace(s))) {
        const rows = spaceRows(space, byAddress, section.relayId);
        if (rows.length) out.push({ key: `hc:${section.relayId}:${space.id}`, space, relayId: section.relayId, rows });
      }
    }
    return out;
  });

  const pinnedSpaceGroups = $derived.by(() => {
    const byKey = new Map(spaceGroups.map((g) => [g.key, g]));
    const out: HcSpaceGroup[] = [];
    for (const key of $pinnedHcSpaces) {
      const group = byKey.get(key);
      if (group) out.push(group);
    }
    return out;
  });

  // Drop pins whose space vanished while its relay is connected — a closed
  // space's pin must not linger to re-match a recycled id. Pins on
  // disconnected relays are kept; the space may come back with the link.
  $effect(() => {
    const live = new Set(spaceGroups.map((g) => g.key));
    const connected = new Set(
      sections.filter((s) => s.connected).map((s) => s.relayId),
    );
    prunePinnedHcSpaces(live, connected);
  });


  // Federation aliases: each daemon mirrors remote machines' terminals as
  // "<machine>/<suffix>" next to its own bare "<suffix>" terminals. When both
  // machines' relays are paired, a qualified card that the owning relay also
  // publishes bare would render the same physical terminal twice — hide the
  // alias and let the owner's card carry it.
  const hiddenPaneIds = $derived.by(() => {
    const bareByRelay = new Map<string, Set<string>>();
    for (const [relayId, inv] of $hcInventories) {
      const bare = new Set<string>();
      for (const agent of inv.agents || []) {
        const id = agent.terminal_id || '';
        if (id && !id.includes('/')) bare.add(id);
      }
      bareByRelay.set(relayId, bare);
    }
    // A relay's federation name is the qualified prefix that this relay's
    // machines[] names and that collides with its bare suffixes.
    const fedName = new Map<string, string>();
    for (const [relayId, inv] of $hcInventories) {
      const machines = new Set(inv.machines || []);
      for (const agent of inv.agents || []) {
        const id = agent.terminal_id || '';
        const slash = id.indexOf('/');
        if (slash < 1 || !machines.has(id.slice(0, slash))) continue;
        const suffix = id.slice(slash + 1);
        for (const [other, bare] of bareByRelay) {
          if (other !== relayId && bare.has(suffix) && !fedName.has(other)) {
            fedName.set(other, id.slice(0, slash));
          }
        }
      }
    }
    const hidden = new Set<string>();
    const seenQualified = new Set<string>();
    for (const [relayId, inv] of $hcInventories) {
      for (const agent of inv.agents || []) {
        const id = agent.terminal_id || '';
        const slash = id.indexOf('/');
        if (slash < 1) continue;
        const machine = id.slice(0, slash);
        const suffix = id.slice(slash + 1);
        const key = `${machine}/${suffix}`;
        let hide = seenQualified.has(key);
        if (!hide) {
          for (const [other, bare] of bareByRelay) {
            if (other !== relayId && bare.has(suffix) && fedName.get(other) === machine) {
              hide = true;
              break;
            }
          }
        }
        if (hide) hidden.add(`${relayId}:${agent.pane_id}`);
        else seenQualified.add(key);
      }
    }
    return hidden;
  });

  function visible(agent: Agent, relayId: string): boolean {
    return !hiddenPaneIds.has(`${relayId}:${agent.pane_id}`);
  }
  // Terminal cards are indexed by the addresses the space topology references.
  function agentsByAddress(agents: Agent[] | undefined): Map<string, Agent> {
    const map = new Map<string, Agent>();
    for (const agent of agents || []) {
      if (agent.terminal_id) map.set(agent.terminal_id, agent);
    }
    return map;
  }

  // Space rows pair each tab with its synthesized agent record.
  function spaceRows(space: HcSpace, byAddress: Map<string, Agent>, relayId: string): { tab: HcTab; agent: Agent }[] {
    const rows: { tab: HcTab; agent: Agent }[] = [];
    for (const tab of space.tabs || []) {
      for (const term of tab.terminals || []) {
        const agent = byAddress.get(term.terminal);
        if (agent && visible(agent, relayId)) rows.push({ tab, agent });
      }
    }
    return rows;
  }

  // Synthetic "hc-core:<address>" spaces mark terminals the window does not
  // expose (unlabeled hc workers).
  function isCoreSpace(space: HcSpace): boolean {
    return Boolean(space.id?.startsWith('hc-core:'));
  }

  function coreAddress(space: HcSpace): string {
    return space.id.slice('hc-core:'.length);
  }

  // Visible core-terminal agents for a section (federation aliases removed).
  function coreAgentsFor(inventory: HcInventory, byAddress: Map<string, Agent>, relayId: string): Agent[] {
    const agents: Agent[] = [];
    for (const space of inventory.spaces || []) {
      if (!isCoreSpace(space)) continue;
      const agent = byAddress.get(coreAddress(space));
      if (agent && visible(agent, relayId)) agents.push(agent);
    }
    return agents;
  }

  // Group a section's VISIBLE window spaces by owning machine: local
  // spaces (machine unset) first, then one block per federated machine —
  // the visual boundary that keeps "laptop" and "desktop" cards apart.
  // A space contributes only when it has visible rows and isn't pinned
  // (pinned spaces render in the Pinned section instead). A machine
  // block whose every space was hidden — e.g. the owning relay already
  // publishes those terminals bare, so the dedupe hid all their cards —
  // emits nothing: a bare @machine heading over empty space reads as a
  // stray marker, not a boundary.
  function machineGroups(section: { inventory: HcInventory; relayId: string }): { machine: string; groups: HcSpaceGroup[] }[] {
    const byAddress = agentsByAddress(section.inventory.agents);
    const local: HcSpaceGroup[] = [];
    const byMachine = new Map<string, HcSpaceGroup[]>();
    for (const space of (section.inventory.spaces || []).filter((s) => !isCoreSpace(s))) {
      const rows = spaceRows(space, byAddress, section.relayId);
      const key = `hc:${section.relayId}:${space.id}`;
      if (!rows.length || pinnedSpaces.has(key)) continue;
      const group: HcSpaceGroup = { key, space, relayId: section.relayId, rows };
      if (!space.machine) {
        local.push(group);
      } else {
        const list = byMachine.get(space.machine) || [];
        list.push(group);
        byMachine.set(space.machine, list);
      }
    }
    const out: { machine: string; groups: HcSpaceGroup[] }[] = [];
    if (local.length) out.push({ machine: '', groups: local });
    for (const [machine, groups] of [...byMachine.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
      out.push({ machine, groups });
    }
    return out;
  }

  // Hold-to-close: a long press on a card offers agent_stop (hc kill) with a
  // confirm, mirroring the legacy list's hold gesture. Holding a space
  // summary offers workspace_close (`herdr-app do space.close`), the same
  // gesture legacy workspaces use.
  const CLOSE_PRESS_MS = 550;
  const CLOSE_SLOP_PX = 12;
  type CloseTarget = { agent: Agent; kind: 'agent' | 'space'; label?: string };
  let closeDialogOpen = $state(false);
  let closeTarget = $state<CloseTarget | null>(null);
  let closeBusy = $state(false);
  let suppressOpen = $state(false);
  function closePress(node: HTMLElement, target: CloseTarget) {
    let current = target;
    let timer = 0;
    let pointerId = -1;
    let startX = 0;
    let startY = 0;

    function reset() {
      if (timer) { clearTimeout(timer); timer = 0; }
      pointerId = -1;
    }
    function onPointerDown(event: PointerEvent) {
      suppressOpen = false;
      if (!event.isPrimary || event.button !== 0 || closeDialogOpen || closeBusy) return;
      pointerId = event.pointerId;
      startX = event.clientX;
      startY = event.clientY;
      timer = window.setTimeout(() => {
        timer = 0;
        suppressOpen = true;
        navigator.vibrate?.(12);
        closeTarget = current;
        closeDialogOpen = true;
      }, CLOSE_PRESS_MS);
    }
    function onPointerMove(event: PointerEvent) {
      if (event.pointerId !== pointerId) return;
      if (Math.hypot(event.clientX - startX, event.clientY - startY) > CLOSE_SLOP_PX) reset();
    }
    function onPointerEnd(event: PointerEvent) {
      if (event.pointerId === pointerId) reset();
    }
    function onClick(event: MouseEvent) {
      if (!suppressOpen) return;
      suppressOpen = false;
      event.preventDefault();
      event.stopPropagation();
    }
    node.addEventListener('pointerdown', onPointerDown);
    node.addEventListener('pointermove', onPointerMove);
    node.addEventListener('pointerup', onPointerEnd);
    node.addEventListener('pointercancel', onPointerEnd);
    node.addEventListener('click', onClick, true);
    return {
      update(next: CloseTarget) { current = next; },
      destroy() {
        node.removeEventListener('pointerdown', onPointerDown);
        node.removeEventListener('pointermove', onPointerMove);
        node.removeEventListener('pointerup', onPointerEnd);
        node.removeEventListener('pointercancel', onPointerEnd);
        node.removeEventListener('click', onClick, true);
        reset();
      },
    };
  }

  async function confirmClose() {
    const target = closeTarget;
    if (!target || closeBusy) return;
    closeBusy = true;
    try {
      if (target.kind === 'space') {
        await relayStore.sendToAgent(target.agent, { type: 'workspace_close' });
        relayStore.showToast(`Closed ${target.label || 'space'}.`);
      } else {
        await relayStore.sendToAgent(target.agent, { type: 'agent_stop' });
        relayStore.showToast(`Stopped ${displayName(target.agent)}.`);
      }
      closeDialogOpen = false;
      closeTarget = null;
    } catch (error) {
      relayStore.showToast((error as Error).message, true);
    } finally {
      closeBusy = false;
    }
  }

  async function respond(agent: Agent, index: number, total: number, option: string) {
    await relayStore.respond(agent, index, total, option);
  }
</script>

{#snippet folderIcon()}
  <svg class="path-symbol" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path d="M3 5.5h7l2 2h9v11H3z"></path>
  </svg>
{/snippet}

{#snippet hcCard(agent: Agent, relayId: string, label: string | null, showMachine: boolean)}
  {@const interaction = questionInteraction(agent)}
  {@const options = approvalOptions(agent)}
  {@const group = agentStatusGroup(agent)}
  {@const tone = agentStatusTone(agent)}
  {@const blocked = group === 'blocked'}
  {@const needsInspection = group === 'attention'}
  {@const age = relativeAge(agent)}
  {@const agentPath = relayPath(agent.relay_id || relayId, String(agent.cwd || ''))}
  {@const wt = worktreeTailStart(agentPath, String(agent.cwd || ''), '')}
  {@const inventoryReady = !connections.has(agent.relay_id || relayId) || connections.get(agent.relay_id || relayId)?.inventory.state === 'ready'}
  <article class:blocked class="agent-card compact-agent-card" class:stale={!inventoryReady} class:card-live={tone === 'success'}>
    <button
      class="agent-open"
      aria-label={`Open ${label || tabName(agent) || displayName(agent)} on ${relayId}`}
      disabled={!inventoryReady}
      title={!inventoryReady ? 'This terminal is unavailable until the relay recovers.' : 'Hold to stop this terminal.'}
      use:closePress={{ agent, kind: 'agent' }}
      onclick={() => { if (!suppressOpen) onopen(agent); }}
    >
      <span class="agent-identity">
        <AgentLogo agent={agent.agent} />
        <span class={`status-dot status-${tone}`} class:hollow={group === 'ready'} aria-hidden="true"></span>
      </span>
      <span class="agent-copy">
        <span class="agent-title-row">
          <span class="agent-tab-name">{label || tabName(agent) || displayName(agent)}{#if showMachine && agent.terminal_id?.includes('/')} <span class="host-badge">@{agent.terminal_id.split('/')[0]}</span>{/if}</span>
          {#if age}
            <time class="agent-age" datetime={new Date(agentLastActiveAt(agent)).toISOString()} title={new Date(agentLastActiveAt(agent)).toLocaleString()}>{age}</time>
          {/if}
          {#if agentPath}
            <span class="agent-path">{@render folderIcon()}<span>{#if wt >= 0}{agentPath.slice(0, wt)}<strong class="agent-path-tail">{agentPath.slice(wt)}</strong>{:else}{agentPath}{/if}</span></span>
          {/if}
        </span>
        {#if agent.agent_role || agent.agent_model || agent.agent_effort}
          <span class="agent-chips" aria-label={`${agent.agent_role || ''} ${agent.agent_model || ''} ${agent.agent_effort || ''}`.trim()}>
            {#if agent.agent_role}<span class="agent-chip agent-chip-role">{agent.agent_role}</span>{/if}
            {#if agent.agent_model}<span class="agent-chip agent-chip-model">{shortModelLabel(agent.agent_model)}</span>{/if}
            {#if agent.agent_effort}<span class="agent-chip agent-chip-effort">{agent.agent_effort}</span>{/if}
          </span>
        {/if}
        {#if blocked || needsInspection}
          <span class="prompt-preview">{interaction?.question || approvalPromptPreview(agent)}</span>
        {/if}
      </span>
    </button>
    <button
      type="button"
      class="agent-pin"
      class:pinned={pinned.has(agent.pane_id)}
      aria-pressed={pinned.has(agent.pane_id)}
      aria-label={pinned.has(agent.pane_id) ? `Unpin ${displayName(agent)}` : `Pin ${displayName(agent)}`}
      title={pinned.has(agent.pane_id) ? 'Unpin conversation' : 'Pin conversation to top'}
      onclick={(event) => { event.stopPropagation(); togglePinnedConversation(agent.pane_id); }}
    >
      <svg viewBox="0 0 24 24" fill={pinned.has(agent.pane_id) ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
        <path d="M12 17v5M9 4h6l1 7 3 3H5l3-3z"></path>
      </svg>
    </button>
    {#if blocked && !$responding.has(agent.pane_id)}
      <div class="agent-actions" aria-label={`Actions for ${displayName(agent)}`}>
        {#if interaction}
          <Button variant="trust" size="sm" onclick={() => onopen(agent)}>
            {interaction.kind === 'multi_select' ? 'Choose options' : 'Choose answer'} ({interaction.options.length})
          </Button>
        {:else}
          {#each options as option, index (`${index}:${option}`)}
            <Button
              variant={approvalButtonTone(option, index, options.length) === 'deny' ? 'danger' : approvalButtonTone(option, index, options.length) === 'trust' ? 'trust' : 'default'}
              size="sm"
              disabled={!inventoryReady}
              onclick={() => respond(agent, index, options.length, option)}
            >{option.length > 48 ? `${option.slice(0, 45)}...` : option}</Button>
          {/each}
        {/if}
      </div>
    {:else if blocked}
      <p class="responding" role="status">Waiting for agent…</p>
    {/if}
  </article>
{/snippet}

<main class="agent-list" aria-label="Herdr App terminals">
  {#if sections.length === 0}
    <div class="empty-state">
      <span class="empty-icon" aria-hidden="true">⌘</span>
      <h2>No Herdr App detected</h2>
      <p>No paired relay reports a herdr-core daemon yet. This view appears automatically on machines running the new app.</p>
    </div>
  {:else}
    {#if pinnedAgents.length || pinnedSpaceGroups.length}
      <section class="agent-section" aria-label="Pinned">
        <h2 class="section-heading">Pinned</h2>
        {#each pinnedSpaceGroups as group (group.key)}
          {@const dKey = `${group.key}:pinned`}
          <details class="workspace-card" open={disclosureOpen(dKey)} ontoggle={(event) => rememberDisclosure(dKey, event)}>
            <summary
              title="Hold to close this space."
              use:closePress={{ agent: group.rows[0].agent, kind: 'space', label: group.space.name || 'Space' }}
            >
              <span class="agent-project">{group.space.name || 'Space'}</span>
              {#if group.space.machine}<span class="agent-count">@{group.space.machine}</span>{/if}
              <button
                type="button"
                class="workspace-pin pinned"
                aria-pressed="true"
                aria-label={`Unpin ${group.space.name || 'space'}`}
                title="Unpin workspace"
                onclick={(event) => { event.preventDefault(); event.stopPropagation(); togglePinnedHcSpace(group.key); }}
              >
                <svg viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M12 17v5M9 4h6l1 7 3 3H5l3-3z"></path></svg>
              </button>
            </summary>
            <div class="agent-grid compact-agent-grid">
              {#each group.rows.filter((row) => !pinned.has(row.agent.pane_id)) as row (row.agent.pane_id)}
                {@render hcCard(row.agent, group.relayId, row.tab.name || null, false)}
              {/each}
            </div>
          </details>
        {/each}
        {#if pinnedAgents.length}
          <div class="workspace-grid">
            <div class="agent-grid compact-agent-grid">
              {#each pinnedAgents as agent (agent.pane_id)}
                {@render hcCard(agent, agent.relay_id, null, true)}
              {/each}
            </div>
          </div>
        {/if}
      </section>
    {/if}
    {#each sections as section (section.relayId)}
      <section class="agent-section">
        <h2 class="section-heading">
          {section.label}
          {#if !section.connected}
            <span class="agent-count">offline</span>
          {:else if section.inventory.state === 'error'}
            <span class="agent-count">{section.inventory.message || 'Herdr App unavailable'}</span>
          {/if}
        </h2>

        {#if section.inventory.state !== 'error'}
          {@const byAddress = agentsByAddress(section.inventory.agents)}
          {@const coreAgents = coreAgentsFor(section.inventory, byAddress, section.relayId)}
          <!-- Group window spaces by owning machine: local (no machine
               label) first, then one block per federated machine so a
               laptop pane can never blend into the desktop's list.
               machineGroups already dropped spaces with no visible rows,
               so an empty machine block — every terminal deduped in
               favour of the owning relay — renders nothing at all. -->
          {@const machineBlocks = machineGroups(section)}
          <div class="workspace-grid">
            {#each machineBlocks as block (block.machine || 'local')}
              {#if block.machine}
                <h3 class="machine-heading" aria-label={`Remote machine ${block.machine}`}>@{block.machine}</h3>
              {/if}
              {#each block.groups as group (group.key)}
                {@const dKey = group.key}
                <details class="workspace-card" open={disclosureOpen(dKey)} ontoggle={(event) => rememberDisclosure(dKey, event)}>
                  <summary
                    title="Hold to close this space."
                    use:closePress={{ agent: group.rows[0].agent, kind: 'space', label: group.space.name || 'Space' }}
                  >
                    <span class="agent-project">{group.space.name || 'Space'}</span>
                    {#if group.space.machine}<span class="agent-count">@{group.space.machine}</span>{/if}
                    <button
                      type="button"
                      class="workspace-pin"
                      class:pinned={pinnedSpaces.has(group.key)}
                      aria-pressed={pinnedSpaces.has(group.key)}
                      aria-label={`Pin ${group.space.name || 'space'}`}
                      title="Pin workspace to top"
                      onclick={(event) => { event.preventDefault(); event.stopPropagation(); togglePinnedHcSpace(group.key); }}
                    >
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false"><path d="M12 17v5M9 4h6l1 7 3 3H5l3-3z"></path></svg>
                    </button>
                  </summary>
                  <div class="agent-grid compact-agent-grid">
                    {#each group.rows.filter((row) => !pinned.has(row.agent.pane_id)) as row (row.agent.pane_id)}
                      {@render hcCard(row.agent, group.relayId, row.tab.name || null, false)}
                    {/each}
                  </div>
                </details>
              {/each}
            {/each}

            {#if coreAgents.length > 0}
              {@const coreKey = `hc:${section.relayId}:core-terminals`}
              <details class="workspace-card" open={disclosureOpen(coreKey)} ontoggle={(event) => rememberDisclosure(coreKey, event)}>
                <summary>
                  <span class="agent-project">Core terminals</span>
                  <span class="agent-count">{coreAgents.length}</span>
                </summary>
                <div class="agent-grid compact-agent-grid">
                  {#each coreAgents.filter((a) => !pinned.has(a.pane_id)) as agent (agent.pane_id)}
                    {@render hcCard(agent, section.relayId, agent.name || null, true)}
                  {/each}
                </div>
              </details>
            {/if}
          </div>
        {:else}
          <p class="hint">{section.inventory.message || 'The Herdr App core did not answer.'}</p>
        {/if}
      </section>
    {/each}
  {/if}
</main>

<AppDialog
  id="hc-view-close"
  bind:open={closeDialogOpen}
  title={closeTarget?.kind === 'space'
    ? `Close ${closeTarget.label || 'space'}?`
    : `Stop ${closeTarget ? (tabName(closeTarget.agent) || displayName(closeTarget.agent)) : 'terminal'}?`}
  description={closeTarget?.kind === 'space'
    ? 'Closes the space and ends every terminal inside it.'
    : 'Ends the program running in this terminal.'}
>
  <div class="dialog-actions">
    <Button variant="secondary" disabled={closeBusy} onclick={() => { closeDialogOpen = false; }}>Cancel</Button>
    <Button variant="danger" disabled={closeBusy} onclick={confirmClose}>{closeBusy ? (closeTarget?.kind === 'space' ? 'Closing…' : 'Stopping…') : (closeTarget?.kind === 'space' ? 'Close' : 'Stop')}</Button>
  </div>
</AppDialog>

<style>
  .agent-list {
    margin: 0 auto;
    max-width: 44rem;
    padding: 0 .85rem 5.5rem;
    width: 100%;
  }
</style>
