/**
 * Wire contract between the browser and the collab service. Imported by both
 * (the collab service bundles it from src/lib), so the two can't drift.
 *
 * Integrity model, in brief:
 *
 *  - Schema: the client sends its schema fingerprint; a mismatch is refused
 *    before any sync, because y-prosemirror deletes content it can't
 *    represent (see src/lib/editor/schema.ts).
 *
 *  - Epoch: every (re)seed of a page's shared document from page_contents
 *    starts a new epoch. A Y.Doc that has held state from one epoch must
 *    never sync into another — merging it would resurrect content that a
 *    version restore (or a re-seed) replaced. The server tells the client its
 *    epoch before sending any content; the client presents it on every
 *    reconnect; the server refuses a mismatch. A client that holds content
 *    but never learned its epoch must discard its Y.Doc.
 *
 *  - Rejected updates: an update the server refuses (invalid for the schema,
 *    too large) closes the connection with InvalidUpdate. The client must not
 *    retry it; it discards its Y.Doc and reloads the server's state.
 */

/** Sent by the client as the Hocuspocus auth token (JSON). */
export interface CollabToken {
	v: 1;
	/** The epoch this client's Y.Doc belongs to, or null for a fresh, empty Y.Doc. */
	epoch: number | null;
	/** schemaFingerprint() of the client's editor schema. */
	fingerprint: string;
}

/** Stateless messages from server to client (JSON). */
export type CollabServerMessage =
	| { type: 'epoch'; epoch: number }
	| { type: 'quarantined' }
	| { type: 'access'; canWrite: boolean };

/**
 * Reasons the server gives when refusing authentication or closing a
 * document connection. The client decides whether to reset, reload or stop
 * based on these.
 */
export const CollabReason = {
	/** The client's Y.Doc belongs to a replaced epoch: discard it and reconnect fresh. */
	StaleEpoch: 'stale-epoch',
	/** The shared document was replaced (e.g. a version was restored): discard and reconnect. */
	Reset: 'reset',
	/** The server refused an update: discard local state and reload from the server. */
	InvalidUpdate: 'invalid-update',
	/** The client's editor schema differs from the server's: reload the app. */
	SchemaMismatch: 'schema-mismatch',
	/** The user may not open this page (any more). */
	Forbidden: 'forbidden',
	/** Collaborative editing is switched off server-side: use the single-writer editor. */
	Disabled: 'disabled',
	/** The document could not be loaded; try again later. */
	Unavailable: 'unavailable'
} as const;
export type CollabReasonValue = (typeof CollabReason)[keyof typeof CollabReason];

const REASONS = new Set<string>(Object.values(CollabReason));

export function isCollabReason(value: unknown): value is CollabReasonValue {
	return typeof value === 'string' && REASONS.has(value);
}

/** Hocuspocus document name for a page. */
export function collabDocumentName(pageId: string): string {
	return `page:${pageId.toLowerCase()}`;
}

// Lowercase only: one page must map to exactly one document name, or two
// in-memory copies of the same page could diverge on the server.
const DOC_NAME = /^page:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/;

/** The page id in a Hocuspocus document name, or null if it isn't one. */
export function pageIdFromDocumentName(name: string): string | null {
	const m = DOC_NAME.exec(name);
	return m ? m[1] : null;
}

export function encodeToken(token: CollabToken): string {
	return JSON.stringify(token);
}

export function decodeToken(raw: string): CollabToken | null {
	try {
		const t = JSON.parse(raw) as Partial<CollabToken>;
		if (t?.v !== 1 || typeof t.fingerprint !== 'string') return null;
		if (t.epoch !== null && !(typeof t.epoch === 'number' && Number.isInteger(t.epoch) && t.epoch > 0)) return null;
		return { v: 1, epoch: t.epoch, fingerprint: t.fingerprint };
	} catch {
		return null;
	}
}

export function decodeServerMessage(raw: string): CollabServerMessage | null {
	try {
		const m = JSON.parse(raw) as CollabServerMessage;
		switch (m?.type) {
			case 'epoch':
				return Number.isInteger(m.epoch) && m.epoch > 0 ? m : null;
			case 'quarantined':
				return m;
			case 'access':
				return typeof m.canWrite === 'boolean' ? m : null;
			default:
				return null;
		}
	} catch {
		return null;
	}
}

/**
 * Whether a URL is safe to keep in a link href. Mirrors the API's isSafeURL
 * (api/internal/handler/content_validator.go): relative references, http(s)
 * and mailto only. The collab service strips anything else from the shared
 * document so every client — not just the next snapshot — is protected.
 */
export function isSafeUrl(raw: unknown): boolean {
	if (typeof raw !== 'string') return false;
	const trimmed = raw.trim();
	if (trimmed === '') return true;
	// Browsers ignore these when resolving a scheme ("java\tscript:").
	// eslint-disable-next-line no-control-regex
	const normalized = trimmed.replace(/[\t\n\r\v\f\u0000]/g, '').toLowerCase();
	const colon = normalized.indexOf(':');
	if (colon === -1) return true;
	for (const sep of ['/', '?', '#']) {
		const i = normalized.indexOf(sep);
		if (i !== -1 && i < colon) return true;
	}
	const scheme = normalized.slice(0, colon);
	return scheme === 'http' || scheme === 'https' || scheme === 'mailto';
}
