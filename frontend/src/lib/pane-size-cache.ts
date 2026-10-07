// Remembers the pane width (and height lease) each phone terminal settled on,
// per pane id. Reopening a terminal seeds the lease from this cache so the
// wrapping layout engages on the first paint instead of waiting for a fresh
// measure → lease → resize → settle round trip. The background lease still
// confirms the size; the cache only removes the startup latency, which is
// always the same because the phone's width never changes between visits.

const STORAGE_KEY = 'herdr_pane_size_cache';
const MAX_ENTRIES = 40;

export interface PaneSizeCacheEntry {
  columns: number;
  rows: number;
}

export function cachedPaneSize(paneId: string): PaneSizeCacheEntry {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { columns: 0, rows: 0 };
    const map = JSON.parse(raw) as Record<string, PaneSizeCacheEntry>;
    const hit = map[paneId];
    if (hit && Number.isInteger(hit.columns) && hit.columns > 0) {
      return { columns: hit.columns, rows: Number.isInteger(hit.rows) && hit.rows > 0 ? hit.rows : 0 };
    }
  } catch {
    // Corrupt or unavailable storage behaves like an empty cache.
  }
  return { columns: 0, rows: 0 };
}

export function storePaneSize(paneId: string, columns: number, rows: number): void {
  if (!Number.isInteger(columns) || columns <= 0) return;
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    const map = (raw ? JSON.parse(raw) : {}) as Record<string, PaneSizeCacheEntry>;
    map[paneId] = { columns, rows };
    const keys = Object.keys(map);
    if (keys.length > MAX_ENTRIES) delete map[keys[0]];
    localStorage.setItem(STORAGE_KEY, JSON.stringify(map));
  } catch {
    // Storage full or blocked: the lease simply re-measures next time.
  }
}
