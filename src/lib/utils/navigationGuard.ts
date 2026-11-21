/** Route of the note page, whose Editor is reused across note-to-note navigations. */
const NOTE_ROUTE = '/notes/[pageId]';

/**
 * Whether a navigation goes from one note to another, so the layout's
 * navigation guard need not hold it until pending writes settle.
 *
 * Moving between notes reuses the mounted Editor: when its pageId changes it
 * flushes the old page's pending writes (keyed by the page they belong to) and
 * only then loads the next page. Holding these navigations added nothing but
 * latency, up to the 500 ms content-save debounce plus the save round-trip,
 * which made quick page flips in the sidebar feel like clicks were ignored
 * (#53). Every other navigation unmounts the component that owns the writes,
 * so it still waits for them to land first.
 */
export function isNoteToNoteNavigation(
	fromRouteId: string | null | undefined,
	toRouteId: string | null | undefined
): boolean {
	return fromRouteId === NOTE_ROUTE && toRouteId === NOTE_ROUTE;
}
