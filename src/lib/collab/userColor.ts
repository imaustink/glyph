/**
 * Collaborator caret colours. Literal hex because the caret extension
 * accepts nothing else (it drops any colour that isn't #rrggbb). The hues
 * avoid the priority/status palette so another person's caret never reads as
 * a state, and all are light enough for --collab-label-text on top.
 */
const COLORS = ['#61afef', '#c678dd', '#56b6c2', '#d19a66', '#e8a3a3', '#9aa5ff'];

/** A stable caret colour for a user (the same person gets the same colour on every device). */
export function collabUserColor(userId: string | null): string {
	if (!userId) return COLORS[0];
	let h = 0;
	for (let i = 0; i < userId.length; i++) h = (h * 31 + userId.charCodeAt(i)) | 0;
	return COLORS[Math.abs(h) % COLORS.length];
}
