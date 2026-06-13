import { DEBOUNCE } from '$lib/models/constants';

export interface HoverPreviewState {
	taskId: string;
	x: number;
	y: number;
}

export interface HoverPreviewHandle {
	/** Get the current hover preview state. */
	getPreview(): HoverPreviewState | null;
	/** Set up hover event listeners on the editor element. */
	setup(editorEl: HTMLElement): void;
	/** Clear and tear down hover state + listeners. */
	destroy(editorEl: HTMLElement | null): void;
}

export function useHoverPreview(): HoverPreviewHandle {
	let hoverPreview: HoverPreviewState | null = null;
	let hoverTimer: ReturnType<typeof setTimeout> | null = null;

	function handleMouseover(e: MouseEvent) {
		const target = e.target as HTMLElement;
		const li = target.closest('[data-task-id]') as HTMLElement | null;
		if (!li) { clear(); return; }

		const taskId = li.getAttribute('data-task-id');
		if (!taskId) return;

		if (hoverTimer) clearTimeout(hoverTimer);
		hoverTimer = setTimeout(() => {
			const rect = li.getBoundingClientRect();
			hoverPreview = { taskId, x: rect.left + rect.width / 2, y: rect.top - 8 };
		}, DEBOUNCE.HOVER_PREVIEW);
	}

	function handleMouseout(e: MouseEvent) {
		const related = e.relatedTarget as HTMLElement | null;
		if (related?.closest('[data-task-id]')) return;
		clear();
	}

	function clear() {
		if (hoverTimer) clearTimeout(hoverTimer);
		hoverPreview = null;
	}

	function getPreview() { return hoverPreview; }

	function setup(editorEl: HTMLElement) {
		editorEl.addEventListener('mouseover', handleMouseover);
		editorEl.addEventListener('mouseout', handleMouseout);
	}

	function destroy(editorEl: HTMLElement | null) {
		if (hoverTimer) clearTimeout(hoverTimer);
		editorEl?.removeEventListener('mouseover', handleMouseover);
		editorEl?.removeEventListener('mouseout', handleMouseout);
	}

	return { getPreview, setup, destroy };
}
