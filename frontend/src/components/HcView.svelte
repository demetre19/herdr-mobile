<script lang="ts">
  import { relayStore } from '$lib/store';
  import { agentStatusTone, agentStatusGroup, displayName, tabName } from '$lib/agents';
  import AgentLogo from './AgentLogo.svelte';
  import type { Agent, HcInventory, HcSpace, HcTab, RelayConfig, RelayConnectionView } from '$lib/types';

  const {
    connections,
    relays,
    onopen,
  }: {
    connections: Map<string, RelayConnectionView>;
    relays: RelayConfig[];
    onopen: (agent: Agent) => void;
  } = $props();

  const hcInventories = relayStore.hcInventories;

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
</script>

<main class="agent-list" aria-label="Herdr App terminals">
  {#if sections.length === 0}
    <div class="empty-state">
      <span class="empty-icon" aria-hidden="true">⌘</span>
      <h2>No Herdr App detected</h2>
      <p>No paired relay reports a herdr-core daemon yet. This view appears automatically on machines running the new app.</p>
    </div>
  {:else}
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
          {@const windowSpaces = (section.inventory.spaces || []).filter((s) => !isCoreSpace(s))}
          {@const coreAgents = coreAgentsFor(section.inventory, byAddress, section.relayId)}
          <div class="workspace-grid">
            {#each windowSpaces as space (space.id)}
              {@const rows = spaceRows(space, byAddress, section.relayId)}
              {#if rows.length > 0}
                <details class="workspace-card" open>
                  <summary>
                    <span class="agent-project">{space.name || 'Space'}</span>
                    {#if space.machine}<span class="agent-count">{space.machine}</span>{/if}
                  </summary>
                  <div class="agent-grid">
                    {#each rows as row (row.agent.pane_id)}
                      {@const tone = agentStatusTone(row.agent)}
                      {@const group = agentStatusGroup(row.agent)}
                      <article class="agent-card" class:card-live={tone === 'success'}>
                        <button
                          class="agent-open"
                          aria-label={`Open ${row.tab.name || tabName(row.agent) || displayName(row.agent)} on ${section.label}`}
                          onclick={() => onopen(row.agent)}
                        >
                          <span class="agent-identity">
                            <AgentLogo agent={row.agent.agent} />
                            <span class={`status-dot status-${tone}`} class:hollow={group === 'ready'} aria-hidden="true"></span>
                          </span>
                          <span class="agent-copy">
                            <span class="agent-title-row">
                              <span class="agent-tab-name">{row.tab.name || tabName(row.agent) || displayName(row.agent)}</span>
                              <span class="agent-age">{row.agent.terminal_id}</span>
                            </span>
                            <span class="agent-meta">
                              {#if row.agent.agent}{row.agent.agent} · {/if}{row.agent.status || 'unknown'}{#if row.agent.cwd} · {row.agent.cwd}{/if}
                            </span>
                          </span>
                        </button>
                      </article>
                    {/each}
                  </div>
                </details>
              {/if}
            {/each}

            {#if coreAgents.length > 0}
              <details class="workspace-card" open>
                <summary>
                  <span class="agent-project">Core terminals</span>
                  <span class="agent-count">{coreAgents.length}</span>
                </summary>
                <div class="agent-grid">
                  {#each coreAgents as agent (agent.pane_id)}
                      {@const tone = agentStatusTone(agent)}
                      {@const group = agentStatusGroup(agent)}
                      <article class="agent-card" class:card-live={tone === 'success'}>
                        <button
                          class="agent-open"
                          aria-label={`Open ${agent.name || agent.terminal_id} on ${section.label}`}
                          onclick={() => onopen(agent)}
                        >
                          <span class="agent-identity">
                            <AgentLogo agent={agent.agent} />
                            <span class={`status-dot status-${tone}`} class:hollow={group === 'ready'} aria-hidden="true"></span>
                          </span>
                          <span class="agent-copy">
                            <span class="agent-title-row">
                              <span class="agent-tab-name">{agent.name || agent.terminal_id}</span>
                              <span class="agent-age">{agent.terminal_id}</span>
                            </span>
                            <span class="agent-meta">
                              {#if agent.agent}{agent.agent} · {/if}{agent.status || 'unknown'}{#if agent.cwd} · {agent.cwd}{/if}
                            </span>
                          </span>
                        </button>
                      </article>
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

<style>
  .agent-list {
    margin: 0 auto;
    max-width: 44rem;
    padding: 0 .85rem 5.5rem;
    width: 100%;
  }
</style>
