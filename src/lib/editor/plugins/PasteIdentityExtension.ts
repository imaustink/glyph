import { Extension } from '@tiptap/core';

export interface PasteIdentityOptions {
	/** Whether a task id seen in pasted content is one of this page's tasks. */
	taskBelongsHere: (taskId: string) => boolean;
}

// Stub: behaviour lands with the fix (DI-10).
export const PasteIdentityExtension = Extension.create<PasteIdentityOptions>({
	name: 'pasteIdentity',
	addOptions() {
		return { taskBelongsHere: () => false };
	}
});
