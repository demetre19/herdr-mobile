<script lang="ts">
  import '../../src/app.css';
  import TerminalView from '$components/TerminalView.svelte';
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
