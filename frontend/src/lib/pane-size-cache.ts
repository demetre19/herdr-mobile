// Remembers the pane size the phone's terminal settled on — GLOBALLY, one
// entry for every pane: the phone's width never changes, so every project's
// pane leases the same columns. Reopening any terminal seeds the lease from
// this cache so the wrapping layout engages on the first paint instead of
// waiting for a fresh measure → lease → resize → settle round trip. The
// background lease still confirms the size; the cache only removes the
// startup latency.

const STORAGE_KEY = 'herdr_pane_size_cache';

export interface PaneSizeCacheEntry {
  columns: number;
  rows: number;
}

export function cachedPaneSize(): PaneSizeCacheEntry {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { columns: 0, rows: 0 };
    const hit = JSON.parse(raw) as PaneSizeCacheEntry;
    if (hit && Number.isInteger(hit.columns) && hit.columns > 0) {
      return { columns: hit.columns, rows: Number.isInteger(hit.rows) && hit.rows > 0 ? hit.rows : 0 };
    }
  } catch {
    // Corrupt or unavailable storage behaves like an empty cache.
  }
  return { columns: 0, rows: 0 };
}

export function storePaneSize(columns: number, rows: number): void {
  if (!Number.isInteger(columns) || columns <= 0) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ columns, rows }));
  } catch {
    // Storage full or blocked: the lease simply re-measures next time.
  }
}

// Measurement jitter guard: font loading and sub-pixel rounding make the
// probed cell width wobble by a column or two between mounts. Wobble must
// not re-trigger the resize-settle wait (and its 'Resizing terminal…'
// placeholder) on every tab switch — snap to the cached size when the
// measurement is within a couple of columns/rows of it.
export function stabilizePaneSize(columns: number, rows: number, cached: PaneSizeCacheEntry): { columns: number; rows: number } {
  let stableColumns = columns;
  let stableRows = rows;
  if (cached.columns > 0 && Math.abs(columns - cached.columns) <= 2) stableColumns = cached.columns;
  if (cached.rows > 0 && rows > 0 && Math.abs(rows - cached.rows) <= 2) stableRows = cached.rows;
  return { columns: stableColumns, rows: stableRows };
}
