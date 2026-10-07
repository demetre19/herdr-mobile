// Per-agent chord menus for the terminal key row. The pane's agent profile
// decides which shortcuts lead the list — the most useful chord of THAT cli
// comes first (OMP's Ctrl+P role switcher, Claude's Esc interrupt, …), the
// universal control chords follow.

export interface AgentChord {
  keys: string[];
  label: string;
  hint: string;
}

export interface AgentChordSet {
  title: string;
  chords: AgentChord[];
}

const COMMON_CHORDS: AgentChord[] = [
  { keys: ['ctrl+c'], label: 'Ctrl+C', hint: 'Interrupt' },
  { keys: ['ctrl+d'], label: 'Ctrl+D', hint: 'EOF / exit' },
  { keys: ['ctrl+z'], label: 'Ctrl+Z', hint: 'Suspend' },
  { keys: ['ctrl+l'], label: 'Ctrl+L', hint: 'Clear screen' },
  { keys: ['ctrl+r'], label: 'Ctrl+R', hint: 'Search history' },
];

function withCommon(chords: AgentChord[]): AgentChord[] {
  const seen: Record<string, true> = {};
  for (const chord of chords) seen[chord.keys[0]] = true;
  return [...chords, ...COMMON_CHORDS.filter((chord) => !seen[chord.keys[0]])];
}

export function agentChords(agentProfile: string | undefined): AgentChordSet {
  const profile = (agentProfile || '').toLowerCase();
  if (profile.includes('omp') || profile === 'pi' || profile.includes('oh my pi')) {
    return {
      title: 'Oh My Pi',
      chords: withCommon([
        { keys: ['ctrl+p'], label: 'Ctrl+P', hint: 'Switch role / model' },
      ]),
    };
  }
  if (profile.includes('claude')) {
    return {
      title: 'Claude Code',
      chords: withCommon([
        { keys: ['Escape'], label: 'Esc', hint: 'Interrupt' },
        { keys: ['shift+tab'], label: 'Shift+Tab', hint: 'Cycle permission mode' },
      ]),
    };
  }
  if (profile.includes('codex')) {
    return {
      title: 'Codex',
      chords: withCommon([
        { keys: ['Escape'], label: 'Esc', hint: 'Interrupt' },
        { keys: ['ctrl+t'], label: 'Ctrl+T', hint: 'Full transcript' },
        { keys: ['ctrl+g'], label: 'Ctrl+G', hint: 'Edit in $EDITOR' },
      ]),
    };
  }
  if (profile.includes('gemini')) {
    return {
      title: 'Gemini CLI',
      chords: withCommon([
        { keys: ['Escape'], label: 'Esc', hint: 'Cancel' },
        { keys: ['ctrl+y'], label: 'Ctrl+Y', hint: 'Toggle auto-approve' },
      ]),
    };
  }
  return { title: 'Terminal chords', chords: COMMON_CHORDS };
}
