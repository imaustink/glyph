/**
 * Plain HTTP routes on the collab service (everything that isn't a WebSocket
 * upgrade):
 *
 *   GET  /collab/healthz
 *   POST /collab/ops/pages/:pageId/remove-list-item   { nodeId }
 *
 * The ops route exists because a whole-document REST write to a
 * collaborative page is refused (it would clobber collaborators). Edits the
 * app makes outside the editor go here instead and are applied to the shared
 * document like any other change.
 */
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Extension, onRequestPayload } from '@hocuspocus/server';
import type { Api } from './api.js';
import type { GlyphCollab } from './extension.js';
import { removeListItem } from './documentRules.js';
import { originAllowed, toHeaders } from './origin.js';
import type { Logger } from './log.js';

const REMOVE_LIST_ITEM = /^\/collab\/ops\/pages\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\/remove-list-item$/;
const MAX_BODY = 16 * 1024;

export class GlyphHttp implements Extension {
	extensionName = 'glyph-http';

	constructor(
		private readonly opts: { collab: GlyphCollab; api: Api; allowedOrigins: string[]; log: Logger }
	) {}

	async onRequest({ request, response }: onRequestPayload) {
		const url = new URL(request.url ?? '/', 'http://localhost');
		try {
			if (request.method === 'GET' && url.pathname === '/collab/healthz') {
				return this.finish(response, 200, { status: 'ok' });
			}
			const m = REMOVE_LIST_ITEM.exec(url.pathname);
			if (m) {
				if (request.method !== 'POST') return this.finish(response, 405, { error: 'method not allowed' });
				return await this.removeListItem(request, response, m[1]);
			}
			return this.finish(response, 404, { error: 'not found' });
		} catch (err) {
			if (err === null) throw err;
			this.opts.log.error('http request failed', { path: url.pathname, err });
			return this.finish(response, 500, { error: 'internal error' });
		}
	}

	private async removeListItem(request: IncomingMessage, response: ServerResponse, pageId: string) {
		const headers = toHeaders(request.headers);
		// Cookie-authenticated and state-changing: require both a same-origin
		// Origin and a custom header (which a cross-site form can't send), as
		// the API's CSRF middleware does.
		if (!originAllowed(headers, this.opts.allowedOrigins) || !headers.get('x-requested-with')) {
			return this.finish(response, 403, { error: 'forbidden' });
		}
		let body: { nodeId?: unknown };
		try {
			body = JSON.parse(await readBody(request)) as { nodeId?: unknown };
		} catch {
			return this.finish(response, 400, { error: 'invalid body' });
		}
		if (typeof body.nodeId !== 'string' || body.nodeId === '' || body.nodeId.length > 200) {
			return this.finish(response, 400, { error: 'nodeId is required' });
		}
		const nodeId = body.nodeId;

		const session = await this.opts.api.authorize(pageId, headers.get('cookie') ?? '');
		if (!session) return this.finish(response, 404, { error: 'not found' });
		if (!session.enabled) return this.finish(response, 409, { error: 'collaborative editing is disabled', code: 'disabled' });
		if (!session.canWrite) return this.finish(response, 403, { error: 'write access denied' });

		const outcome = await this.opts.collab.serverEdit(pageId, (doc) => removeListItem(doc, nodeId, undefined));
		switch (outcome) {
			case 'changed':
			case 'unchanged':
				return this.finish(response, 200, { removed: outcome === 'changed' });
			case 'quarantined':
				return this.finish(response, 409, { error: 'page is read-only pending recovery', code: 'quarantined' });
			case 'unavailable':
				return this.finish(response, 503, { error: 'document unavailable, retry' });
		}
	}

	/** Write the response, then stop Hocuspocus' default handler (by convention, `throw null`). */
	private finish(response: ServerResponse, status: number, body: unknown): never {
		response.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' });
		response.end(JSON.stringify(body));
		throw null;
	}
}

function readBody(request: IncomingMessage): Promise<string> {
	return new Promise((resolve, reject) => {
		let size = 0;
		const chunks: Buffer[] = [];
		request.on('data', (chunk: Buffer) => {
			size += chunk.length;
			if (size > MAX_BODY) {
				reject(new Error('body too large'));
				request.destroy();
				return;
			}
			chunks.push(chunk);
		});
		request.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
		request.on('error', reject);
	});
}
