import { get, writable } from 'svelte/store';
import {
  ACCENT_CUSTOM_KEY,
  ACCENT_KEY,
  ACCENTS,
  APPEARANCE_KEY,
  APPEARANCES,
  DEFAULT_AGENT_VIEW_KEY,
  DEFAULT_DIRECTORY_KEY,
  DIRECTORY_USAGE_KEY,
  FAVORITE_AGENT_KEY,
  PANE_AGENT_VIEW_OVERRIDES_KEY,
  PINNED_CONVERSATIONS_KEY,
  PINNED_WORKSPACES_KEY,
  WORKSPACE_DISCLOSURE_KEY,
  LEGACY_FONT_KEY,
  HOME_LAYOUT_KEY,
  HOME_LAYOUTS,
  INTERFACE_SIZE_KEY,
  INTERFACE_SIZES,
  TERMINAL_HISTORY_KEY,
  TERMINAL_HISTORY_OPTIONS,
  TERMINAL_HEIGHT_LEASE_KEY,
  TERMINAL_WAKE_LOCK_KEY,
  TERMINAL_REFRESH_KEY,
  TERMINAL_REFRESH_OPTIONS,
  THEME_COLORS,
  THEME_KEY,
  THEMES,
  type Accent,
  type AgentView,
  type Appearance,
  type HomeLayout,
  type InterfaceSize,
  type TerminalHistoryLines,
  type TerminalRefreshInterval,
  type Theme,
} from './config';
import { setTerminalScheme } from './terminal';
import { workspaceIdentity } from './workspaces';
import type { Agent } from './types';
import {
  isAgentView,
  paneViewPreferenceKey,
  parsePaneViewPreferenceKey,
  type PaneAgentViewOverrides,
} from './agent-view';

export function readDefaultAgentView(storage?: Pick<Storage, 'getItem'>): AgentView {
  try {
    const value = (storage || localStorage).getItem(DEFAULT_AGENT_VIEW_KEY);
    return isAgentView(value) ? value : 'terminal';
  } catch {
    return 'terminal';
  }
}

export function readPaneAgentViewOverrides(
  storage?: Pick<Storage, 'getItem'>,
): PaneAgentViewOverrides {
  try {
    const raw = (storage || localStorage).getItem(PANE_AGENT_VIEW_OVERRIDES_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const overrides: Record<string, AgentView> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (parsePaneViewPreferenceKey(key) && isAgentView(value)) overrides[key] = value;
    }
    return overrides;
  } catch {
    return {};
  }
}
// Legacy theme names collapse onto the nearest family; the old 'light'
// theme becomes the neutral palette with a light appearance.
const LEGACY_THEME_MAP: Record<string, { theme: Theme; appearance: Appearance }> = {
  dark: { theme: 'neutral', appearance: 'dark' },
  t3: { theme: 'neutral', appearance: 'dark' },
  t3chat: { theme: 'chat', appearance: 'dark' },
  light: { theme: 'neutral', appearance: 'light' },
  nord: { theme: 'ocean', appearance: 'dark' },
  solarized: { theme: 'ocean', appearance: 'dark' },
  rose: { theme: 'iris', appearance: 'dark' },
  latte: { theme: 'neutral', appearance: 'light' },
};

function savedTheme(): Theme {
  const value = localStorage.getItem(THEME_KEY) || '';
  if (THEMES.includes(value as Theme)) return value as Theme;
  return LEGACY_THEME_MAP[value]?.theme || 'neutral';
}

function savedAppearance(): Appearance {
  const stored = localStorage.getItem(APPEARANCE_KEY);
  if (APPEARANCES.includes(stored as Appearance)) return stored as Appearance;
  const legacy = localStorage.getItem(THEME_KEY) || '';
  return LEGACY_THEME_MAP[legacy]?.appearance || 'system';
}

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;

function savedAccent(): Accent {
  const value = localStorage.getItem(ACCENT_KEY);
  return ACCENTS.includes(value as Accent) ? value as Accent : 'theme';
}

function savedCustomAccent(): string {
  const value = localStorage.getItem(ACCENT_CUSTOM_KEY) || '';
  return HEX_COLOR.test(value) ? value : '#e07820';
}

function savedInterfaceSize(): InterfaceSize {
  const value = localStorage.getItem(INTERFACE_SIZE_KEY) || localStorage.getItem(LEGACY_FONT_KEY);
  return INTERFACE_SIZES.includes(value as InterfaceSize) ? value as InterfaceSize : 'compact';
}


function savedTerminalHistoryLines(): TerminalHistoryLines {
  const value = Number(localStorage.getItem(TERMINAL_HISTORY_KEY));
  return TERMINAL_HISTORY_OPTIONS.includes(value as TerminalHistoryLines)
    ? value as TerminalHistoryLines
    : 1_000;
}

function savedTerminalRefreshInterval(): TerminalRefreshInterval {
  const value = Number(localStorage.getItem(TERMINAL_REFRESH_KEY));
  return TERMINAL_REFRESH_OPTIONS.includes(value as TerminalRefreshInterval)
    ? value as TerminalRefreshInterval
    : 250;
}

// Mixed by default: one card per workspace with a state dot reads better than
// three state sections once workspaces carry worktrees, and agents needing
// input stay on top in both layouts.
function savedHomeLayout(): HomeLayout {
  const value = localStorage.getItem(HOME_LAYOUT_KEY);
  return HOME_LAYOUTS.includes(value as HomeLayout) ? value as HomeLayout : 'mixed';
}


export const defaultAgentView = writable<AgentView>(readDefaultAgentView());
export const paneAgentViewOverrides = writable<PaneAgentViewOverrides>(readPaneAgentViewOverrides());
export const theme = writable<Theme>(savedTheme());
export const appearance = writable<Appearance>(savedAppearance());
export const accent = writable<Accent>(savedAccent());
export const customAccent = writable<string>(savedCustomAccent());
export const interfaceSize = writable<InterfaceSize>(savedInterfaceSize());
export const terminalHistoryLines = writable<TerminalHistoryLines>(savedTerminalHistoryLines());
export const terminalRefreshInterval = writable<TerminalRefreshInterval>(savedTerminalRefreshInterval());
export const homeLayout = writable<HomeLayout>(savedHomeLayout());
// Off by default: resizing the shared pane's height strands stale copies of
// inline agents' status bars in the scrollback (the terminal reflows the
// primary buffer before the agent can repaint), so only people who need
// full-screen TUIs to fit the phone opt in.
export const terminalHeightLease = writable<boolean>(
  localStorage.getItem(TERMINAL_HEIGHT_LEASE_KEY) === 'true',
);
export const terminalWakeLock = writable<boolean>(
  localStorage.getItem(TERMINAL_WAKE_LOCK_KEY) === 'true',
);

const appearanceMedia = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null;

function resolvedAppearance(value: Appearance): 'light' | 'dark' {
  if (value === 'system') return appearanceMedia?.matches === false ? 'light' : 'dark';
  return value;
}

function applyTheme(value: Theme): void {
  const resolved = resolvedAppearance(get(appearance));
  document.documentElement.dataset.theme = value;
  document.documentElement.dataset.appearance = resolved;
  document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute('content', THEME_COLORS[value][resolved]);
  setTerminalScheme(resolved);
}
// Relative luminance decides the accent's text color so a light custom color
// gets dark text and a dark one gets light text.
function accentForegroundFor(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16) / 255;
  const g = parseInt(hex.slice(3, 5), 16) / 255;
  const b = parseInt(hex.slice(5, 7), 16) / 255;
  const luminance = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  return luminance > 0.45 ? 'oklch(0.222003 0.03479 328.979)' : 'oklch(0.990339 0.008411 325.64)';
}

function applyAccent(value: Accent): void {
  const root = document.documentElement;
  if (value === 'theme') delete root.dataset.accent;
  else root.dataset.accent = value;
  if (value === 'custom') {
    const hex = get(customAccent);
    root.style.setProperty('--primary', hex);
    root.style.setProperty('--primary-foreground', accentForegroundFor(hex));
  } else {
    root.style.removeProperty('--primary');
    root.style.removeProperty('--primary-foreground');
  }
}

// System appearance follows the OS live so 'system' never needs a reload.
appearanceMedia?.addEventListener?.('change', () => {
  if (get(appearance) === 'system') applyTheme(get(theme));
});

export function setDefaultAgentView(value: AgentView): 'saved' | 'unavailable' {
  if (!isAgentView(value)) return 'unavailable';
  try {
    localStorage.setItem(DEFAULT_AGENT_VIEW_KEY, value);
  } catch {
    return 'unavailable';
  }
  defaultAgentView.set(value);
  return 'saved';
}

export function setPaneAgentView(
  agent: Agent,
  value: AgentView | null,
): 'saved' | 'unavailable' | 'invalid-target' {
  const key = paneViewPreferenceKey(agent);
  if (!key) return 'invalid-target';
  if (value !== null && !isAgentView(value)) return 'unavailable';
  const current = get(paneAgentViewOverrides);
  const next = { ...current } as Record<string, AgentView>;
  if (value === null) delete next[key];
  else next[key] = value;
  const changed = Object.keys(current).length !== Object.keys(next).length
    || Object.entries(next).some(([entryKey, entryValue]) => current[entryKey] !== entryValue);
  if (!changed) return 'saved';
  try {
    if (Object.keys(next).length) localStorage.setItem(PANE_AGENT_VIEW_OVERRIDES_KEY, JSON.stringify(next));
    else localStorage.removeItem(PANE_AGENT_VIEW_OVERRIDES_KEY);
  } catch {
    return 'unavailable';
  }
  paneAgentViewOverrides.set(next);
  return 'saved';
}

export function clearPaneAgentViewOverridesForRelay(relayId: string): 'saved' | 'unavailable' {
  const current = get(paneAgentViewOverrides);
  const next = { ...current } as Record<string, AgentView>;
  let changed = false;
  for (const key of Object.keys(current)) {
    const identity = parsePaneViewPreferenceKey(key);
    if (identity?.[0] !== relayId) continue;
    delete next[key];
    changed = true;
  }
  if (!changed) return 'saved';
  try {
    if (Object.keys(next).length) localStorage.setItem(PANE_AGENT_VIEW_OVERRIDES_KEY, JSON.stringify(next));
    else localStorage.removeItem(PANE_AGENT_VIEW_OVERRIDES_KEY);
  } catch {
    return 'unavailable';
  }
  paneAgentViewOverrides.set(next);
  return 'saved';
}

export function prunePaneAgentViewOverrides(agents: readonly Agent[]): void {
  const current = get(paneAgentViewOverrides);
  const keys = Object.keys(current);
  if (!keys.length) return;
  const live = new Set<string>();
  // A relay with no live agents may be disconnected rather than empty; its
  // overrides stay so per-pane view choices survive a reconnect.
  const liveRelays = new Set<string>();
  for (const agent of agents) {
    liveRelays.add(agent.relay_id);
    const key = paneViewPreferenceKey(agent);
    if (key) live.add(key);
  }
  const next = { ...current } as Record<string, AgentView>;
  let changed = false;
  for (const key of keys) {
    const identity = parsePaneViewPreferenceKey(key);
    if (!identity || !liveRelays.has(identity[0]) || live.has(key)) continue;
    delete next[key];
    changed = true;
  }
  if (!changed) return;
  try {
    if (Object.keys(next).length) localStorage.setItem(PANE_AGENT_VIEW_OVERRIDES_KEY, JSON.stringify(next));
    else localStorage.removeItem(PANE_AGENT_VIEW_OVERRIDES_KEY);
  } catch {
    return;
  }
  paneAgentViewOverrides.set(next);
}

export function setTheme(value: Theme): void {
  localStorage.setItem(THEME_KEY, value);
  applyTheme(value);
  theme.set(value);
}

export function setCustomAccent(value: string): void {
  if (!HEX_COLOR.test(value)) return;
  localStorage.setItem(ACCENT_CUSTOM_KEY, value);
  customAccent.set(value);
  if (get(accent) === 'custom') applyAccent('custom');
}

export function setAppearance(value: Appearance): void {
  localStorage.setItem(APPEARANCE_KEY, value);
  appearance.set(value);
  applyTheme(get(theme));
}

export function setAccent(value: Accent): void {
  localStorage.setItem(ACCENT_KEY, value);
  applyAccent(value);
  accent.set(value);
}

export function setInterfaceSize(value: InterfaceSize): void {
  localStorage.setItem(INTERFACE_SIZE_KEY, value);
  interfaceSize.set(value);
  document.documentElement.dataset.interfaceSize = value;
}


export function setTerminalHistoryLines(value: TerminalHistoryLines): void {
  localStorage.setItem(TERMINAL_HISTORY_KEY, String(value));
  terminalHistoryLines.set(value);
}

export function setTerminalRefreshInterval(value: TerminalRefreshInterval): void {
  localStorage.setItem(TERMINAL_REFRESH_KEY, String(value));
  terminalRefreshInterval.set(value);
}

export function setTerminalHeightLease(value: boolean): void {
  localStorage.setItem(TERMINAL_HEIGHT_LEASE_KEY, String(value));
  terminalHeightLease.set(value);
}
export function setTerminalWakeLock(value: boolean): void {
  localStorage.setItem(TERMINAL_WAKE_LOCK_KEY, String(value));
  terminalWakeLock.set(value);
}


export function setHomeLayout(value: HomeLayout): void {
  localStorage.setItem(HOME_LAYOUT_KEY, value);
  homeLayout.set(value);
}


export function initializePreferences(): void {
  theme.subscribe(applyTheme)();
  accent.subscribe(applyAccent)();
  interfaceSize.subscribe((value) => { document.documentElement.dataset.interfaceSize = value; })();
}

// --- Pinned workspaces (FR1) -------------------------------------------------

export function readPinnedWorkspaces(storage?: Pick<Storage, 'getItem'>): string[] {
  try {
    const raw = (storage || localStorage).getItem(PINNED_WORKSPACES_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((key): key is string => typeof key === 'string' && key.length > 0);
  } catch {
    return [];
  }
}

export const pinnedWorkspaces = writable<string[]>(readPinnedWorkspaces());

export function togglePinnedWorkspace(key: string): 'saved' | 'unavailable' {
  if (!key) return 'unavailable';
  const current = get(pinnedWorkspaces);
  const next = current.includes(key)
    ? current.filter((entry) => entry !== key)
    : [...current, key];
  try {
    if (next.length) localStorage.setItem(PINNED_WORKSPACES_KEY, JSON.stringify(next));
    else localStorage.removeItem(PINNED_WORKSPACES_KEY);
  } catch {
    return 'unavailable';
  }
  pinnedWorkspaces.set(next);
  return 'saved';
}

/**
 * Persist a reordered pinned-workspace list. The pin array is the display
 * order: drag ordering rewrites it wholesale rather than diffing entries.
 */
export function reorderPinnedWorkspaces(order: string[]): 'saved' | 'unavailable' {
  const next = order.filter((key) => key);
  try {
    if (next.length) localStorage.setItem(PINNED_WORKSPACES_KEY, JSON.stringify(next));
    else localStorage.removeItem(PINNED_WORKSPACES_KEY);
  } catch {
    return 'unavailable';
  }
  pinnedWorkspaces.set(next);
  return 'saved';
}

/**
 * Drop workspace pins whose target no longer exists on a connected relay.
 * The desktop is the source of truth: once a workspace is closed there, its
 * pin must not survive to re-match a later workspace in the same directory
 * (workspaceIdentity falls back to cwd), which is what made a closed item
 * "keep popping" back as an empty workspace. Pins for a relay with no live
 * snapshot are kept — it may be disconnected rather than empty.
 */
export function prunePinnedWorkspaces(
  agents: readonly Agent[],
  workspaces: readonly { relay_id: string; workspace_id: string }[],
): void {
  const current = get(pinnedWorkspaces);
  if (!current.length) return;
  const liveRelays = new Set<string>();
  const liveKeys = new Set<string>();
  for (const agent of agents) {
    liveRelays.add(agent.relay_id);
    liveKeys.add(workspaceIdentity(agent));
  }
  for (const workspace of workspaces) {
    liveRelays.add(workspace.relay_id);
    liveKeys.add(`${workspace.relay_id}\u0000${workspace.workspace_id}`);
  }
  const next = current.filter((key) => {
    const relayId = key.slice(0, key.indexOf('\u0000'));
    if (!liveRelays.has(relayId)) return true;
    return liveKeys.has(key);
  });
  if (next.length === current.length) return;
  try {
    if (next.length) localStorage.setItem(PINNED_WORKSPACES_KEY, JSON.stringify(next));
    else localStorage.removeItem(PINNED_WORKSPACES_KEY);
  } catch {
    return;
  }
  pinnedWorkspaces.set(next);
}

// --- Pinned conversations ----------------------------------------------------

export function readPinnedConversations(storage?: Pick<Storage, 'getItem'>): string[] {
  try {
    const raw = (storage || localStorage).getItem(PINNED_CONVERSATIONS_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((key): key is string => typeof key === 'string' && key.length > 0);
  } catch {
    return [];
  }
}

export const pinnedConversations = writable<string[]>(readPinnedConversations());

export function togglePinnedConversation(paneId: string): 'saved' | 'unavailable' {
  if (!paneId) return 'unavailable';
  const current = get(pinnedConversations);
  const next = current.includes(paneId)
    ? current.filter((entry) => entry !== paneId)
    : [paneId, ...current];
  try {
    if (next.length) localStorage.setItem(PINNED_CONVERSATIONS_KEY, JSON.stringify(next));
    else localStorage.removeItem(PINNED_CONVERSATIONS_KEY);
  } catch {
    return 'unavailable';
  }
  pinnedConversations.set(next);
  return 'saved';
}

/**
 * Persist a reordered pinned-conversation list. Unlike toggles this writes the
 * whole array — drag ordering is the source of truth for display order.
 */
export function reorderPinnedConversations(order: string[]): 'saved' | 'unavailable' {
  const next = order.filter((paneId) => paneId);
  try {
    if (next.length) localStorage.setItem(PINNED_CONVERSATIONS_KEY, JSON.stringify(next));
    else localStorage.removeItem(PINNED_CONVERSATIONS_KEY);
  } catch {
    return 'unavailable';
  }
  pinnedConversations.set(next);
  return 'saved';
}

/**
 * Drop conversation pins whose pane no longer exists on a connected relay.
 * Same source-of-truth rule as workspaces: a closed pane's pin must not
 * linger to re-match a recycled pane id. Pins for a relay with no live
 * snapshot are kept — it may be disconnected rather than empty.
 */
export function prunePinnedConversations(agents: readonly Agent[]): void {
  const current = get(pinnedConversations);
  if (!current.length) return;
  const liveRelays = new Set<string>();
  const livePanes = new Set<string>();
  for (const agent of agents) {
    liveRelays.add(agent.relay_id);
    livePanes.add(agent.pane_id);
  }
  const next = current.filter((paneId) => {
    const relayId = paneId.slice(0, paneId.indexOf('::'));
    if (!liveRelays.has(relayId)) return true;
    return livePanes.has(paneId);
  });
  if (next.length === current.length) return;
  try {
    if (next.length) localStorage.setItem(PINNED_CONVERSATIONS_KEY, JSON.stringify(next));
    else localStorage.removeItem(PINNED_CONVERSATIONS_KEY);
  } catch {
    return;
  }
  pinnedConversations.set(next);
}

// --- Workspace disclosure persistence (FR4) ----------------------------------

export function readWorkspaceDisclosure(storage?: Pick<Storage, 'getItem'>): Record<string, boolean> {
  try {
    const raw = (storage || localStorage).getItem(WORKSPACE_DISCLOSURE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const out: Record<string, boolean> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === 'boolean') out[key] = value;
    }
    return out;
  } catch {
    return {};
  }
}

export function persistWorkspaceDisclosure(map: Record<string, boolean>): 'saved' | 'unavailable' {
  try {
    if (Object.keys(map).length) localStorage.setItem(WORKSPACE_DISCLOSURE_KEY, JSON.stringify(map));
    else localStorage.removeItem(WORKSPACE_DISCLOSURE_KEY);
  } catch {
    return 'unavailable';
  }
  return 'saved';
}

// --- Per-relay default directory (FR3) ---------------------------------------

export function readDefaultDirectories(storage?: Pick<Storage, 'getItem'>): Record<string, string> {
  try {
    const raw = (storage || localStorage).getItem(DEFAULT_DIRECTORY_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const out: Record<string, string> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === 'string' && value.trim()) out[key] = value;
    }
    return out;
  } catch {
    return {};
  }
}

export const defaultDirectories = writable<Record<string, string>>(readDefaultDirectories());

export function setDefaultDirectory(relayId: string, path: string): 'saved' | 'unavailable' {
  if (!relayId) return 'unavailable';
  const current = get(defaultDirectories);
  const next = { ...current };
  const trimmed = path.trim();
  if (trimmed) next[relayId] = trimmed;
  else delete next[relayId];
  const changed = Object.keys(current).length !== Object.keys(next).length
    || Object.entries(next).some(([key, value]) => current[key] !== value);
  if (!changed) return 'saved';
  try {
    if (Object.keys(next).length) localStorage.setItem(DEFAULT_DIRECTORY_KEY, JSON.stringify(next));
    else localStorage.removeItem(DEFAULT_DIRECTORY_KEY);
  } catch {
    return 'unavailable';
  }
  defaultDirectories.set(next);
  return 'saved';
}

// --- Per-relay directory usage (launch quick-picks) ---------------------------

export interface DirectoryUsageEntry {
  path: string;
  count: number;
  lastUsed: number;
}

type DirectoryUsageMap = Record<string, DirectoryUsageEntry[]>;

function readDirectoryUsage(storage?: Pick<Storage, 'getItem'>): DirectoryUsageMap {
  try {
    const raw = (storage || localStorage).getItem(DIRECTORY_USAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const out: DirectoryUsageMap = {};
    for (const [relay, list] of Object.entries(parsed)) {
      if (!Array.isArray(list)) continue;
      const entries = list.filter((item): item is DirectoryUsageEntry => (
        Boolean(item) && typeof item === 'object'
        && typeof (item as DirectoryUsageEntry).path === 'string'
        && typeof (item as DirectoryUsageEntry).count === 'number'
      ));
      if (entries.length) out[relay] = entries;
    }
    return out;
  } catch {
    return {};
  }
}

export const directoryUsage = writable<DirectoryUsageMap>(readDirectoryUsage());

/** Top directories for a relay: most-used first, ties broken by recency. */
export function topDirectories(relayId: string, limit = 8): DirectoryUsageEntry[] {
  const list = get(directoryUsage)[relayId] || [];
  return [...list]
    .sort((a, b) => b.count - a.count || b.lastUsed - a.lastUsed)
    .slice(0, limit);
}

/** Record a launched working directory so the picker can surface it later. */
export function recordDirectoryUse(relayId: string, path: string): void {
  const trimmed = path.trim();
  if (!relayId || !trimmed) return;
  const map = get(directoryUsage);
  const list = [...(map[relayId] || [])];
  const existing = list.find((entry) => entry.path === trimmed);
  if (existing) {
    existing.count += 1;
    existing.lastUsed = Date.now();
  } else {
    list.push({ path: trimmed, count: 1, lastUsed: Date.now() });
  }
  // Cap per relay so the map can't grow unboundedly; drop least-used tail.
  list.sort((a, b) => b.count - a.count || b.lastUsed - a.lastUsed);
  const next = { ...map, [relayId]: list.slice(0, 32) };
  try {
    localStorage.setItem(DIRECTORY_USAGE_KEY, JSON.stringify(next));
  } catch {
    return;
  }
  directoryUsage.set(next);
}

// --- Favorite launch agent ---------------------------------------------------

export function readFavoriteAgent(storage?: Pick<Storage, 'getItem'>): string {
  try {
    return (storage || localStorage).getItem(FAVORITE_AGENT_KEY) || '';
  } catch {
    return '';
  }
}

export const favoriteAgentProfile = writable<string>(readFavoriteAgent());

/**
 * The pinned agent profile for Start Agent. Stored by profile id, which is
 * stable across relays, so one favorite preselects on every computer that
 * offers it. Clearing stores an empty string.
 */
export function setFavoriteAgent(profileId: string): 'saved' | 'unavailable' {
  const value = profileId.trim();
  if (get(favoriteAgentProfile) === value) return 'saved';
  try {
    if (value) localStorage.setItem(FAVORITE_AGENT_KEY, value);
    else localStorage.removeItem(FAVORITE_AGENT_KEY);
  } catch {
    return 'unavailable';
  }
  favoriteAgentProfile.set(value);
  return 'saved';
}
