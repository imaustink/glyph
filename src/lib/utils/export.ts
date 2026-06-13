/**
 * Data export/import utilities for localStorage mode.
 *
 * Exports all glyph:* keys as a single JSON blob that can be saved as a file.
 * Import restores from the same format.
 */

const GLYPH_PREFIX = 'glyph:';

export interface ExportData {
  version: 1;
  exportedAt: string;
  data: Record<string, unknown>;
}

/**
 * Export all glyph data from localStorage as a downloadable JSON blob.
 */
export function exportAllData(): ExportData {
  const data: Record<string, unknown> = {};

  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (!key || !key.startsWith(GLYPH_PREFIX)) continue;
    try {
      data[key] = JSON.parse(localStorage.getItem(key)!);
    } catch {
      data[key] = localStorage.getItem(key);
    }
  }

  return {
    version: 1,
    exportedAt: new Date().toISOString(),
    data
  };
}

/**
 * Trigger a browser download of the exported data.
 */
export function downloadExport(): void {
  const exportData = exportAllData();
  const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `glyph-backup-${new Date().toISOString().slice(0, 10)}.json`;
  a.click();
  // Defer revoke to give the browser time to start the download.
  // Revoking synchronously after click() can silently cancel the download in some browsers.
  setTimeout(() => URL.revokeObjectURL(url), 100);
}

/**
 * Import data from a previously exported JSON blob.
 * Overwrites any existing keys that conflict.
 */
export function importData(exportData: ExportData): { imported: number; errors: string[] } {
  if (exportData.version !== 1) {
    return { imported: 0, errors: [`Unsupported export version: ${exportData.version}`] };
  }

  const errors: string[] = [];
  let imported = 0;

  for (const [key, value] of Object.entries(exportData.data)) {
    if (!key.startsWith(GLYPH_PREFIX)) {
      errors.push(`Skipped non-glyph key: ${key}`);
      continue;
    }
    try {
      localStorage.setItem(key, JSON.stringify(value));
      imported++;
    } catch (e) {
      errors.push(`Failed to write ${key}: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  return { imported, errors };
}

/**
 * Estimate current storage usage as a percentage of available quota.
 * Returns a value between 0 and 1 (e.g., 0.8 = 80% used).
 * Returns null if estimation is not possible.
 */
export function estimateStorageUsage(): { usedBytes: number; percentage: number } | null {
  if (typeof localStorage === 'undefined') return null;

  let totalBytes = 0;
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (!key) continue;
    const value = localStorage.getItem(key);
    if (value) {
      // Each char in JS is 2 bytes (UTF-16)
      totalBytes += (key.length + value.length) * 2;
    }
  }

  // Most browsers allow ~5MB for localStorage
  const estimatedQuota = 5 * 1024 * 1024;
  return {
    usedBytes: totalBytes,
    percentage: totalBytes / estimatedQuota
  };
}
