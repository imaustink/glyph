/**
 * Device-local user preferences.
 *
 * These are opt-in UI toggles that live on the current device (localStorage),
 * not on the account — they change how the app is displayed, never the stored
 * documents. Keeping them here means a new preference is one field plus one
 * getter/setter, and every screen reads the same reactive value.
 */
import { browser } from '$app/environment';

const STORAGE_KEY = 'glyph:preferences';

export interface Preferences {
  /**
   * Show an OG/URL preview card beneath a link that sits alone on its own line
   * in a note. Opt-in (default off) so existing notes render exactly as before
   * until the user turns it on.
   */
  urlPreviews: boolean;
}

const DEFAULTS: Preferences = {
  urlPreviews: false
};

function load(): Preferences {
  if (!browser) return { ...DEFAULTS };
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULTS };
    const parsed = JSON.parse(raw) as Partial<Preferences>;
    return { ...DEFAULTS, ...parsed };
  } catch {
    // Corrupt or unavailable storage — fall back to defaults rather than crash.
    return { ...DEFAULTS };
  }
}

function createPreferencesStore() {
  const prefs = $state<Preferences>(load());

  function persist() {
    if (!browser) return;
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
    } catch {
      // Storage full or blocked (private mode) — keep the in-memory value.
    }
  }

  return {
    get urlPreviews() {
      return prefs.urlPreviews;
    },
    set urlPreviews(value: boolean) {
      prefs.urlPreviews = value;
      persist();
    }
  };
}

export const preferencesStore = createPreferencesStore();
