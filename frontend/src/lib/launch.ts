// Agent names are display labels, not identifiers — the relay accepts any
// trimmed label up to 64 runes without control characters ("THE BOSS" works).
export function validAgentName(value: unknown): boolean {
  const s = String(value || '').trim();
  if (!s || [...s].length > 64) return false;
  return ![...s].some(c => c < ' ' || c === '');
}

export function launchNamePart(value: unknown, fallback: string): string {
  const normalized = String(value || '').normalize('NFKD').replace(/[\u0300-\u036f]/g, '').toLowerCase();
  const cleaned = normalized.replace(/[^a-z0-9_-]+/g, '-').replace(/^[-_]+|[-_]+$/g, '');
  if (!cleaned) return fallback;
  return /^[a-z]/.test(cleaned) ? cleaned : `${fallback}-${cleaned}`;
}

export function suggestedLaunchName(cwd: string, profileId: string): string {
  const parts = String(cwd || '').replace(/[\\/]+$/, '').split(/[\\/]/).filter(Boolean);
  const directory = launchNamePart(parts.pop(), 'project');
  const agent = launchNamePart(profileId, 'agent');
  const suffix = `-${agent.slice(0, 12)}`;
  return `${directory.slice(0, Math.max(1, 32 - suffix.length))}${suffix}`;
}
