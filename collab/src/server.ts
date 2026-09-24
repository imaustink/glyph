import { Server } from '@hocuspocus/server';
import { documentSchema, schemaFingerprint } from '$lib/editor/schema';
import { loadConfig } from './config.js';
import { GlyphCollab } from './extension.js';
import { GlyphHttp } from './http.js';
import { HttpApi } from './api.js';
import { PgPersistence } from './persistence.js';
import { listen } from './notify.js';
import { jsonLogger } from './log.js';

const config = loadConfig();
const log = jsonLogger();
const schema = documentSchema();
const fingerprint = schemaFingerprint(schema);

const persistence = PgPersistence.fromUrl(config.databaseUrl);
const api = new HttpApi(config.apiUrl, config.serviceToken);
const collab = new GlyphCollab({
	persistence,
	api,
	schema,
	fingerprint,
	allowedOrigins: config.allowedOrigins,
	maxDocumentBytes: config.maxDocumentBytes,
	compactEvery: config.compactEvery,
	log
});
const http = new GlyphHttp({ collab, api, allowedOrigins: config.allowedOrigins, log });

const stopListening = listen(config.databaseUrl, (n) => collab.onReset(n.pageId), (msg, err) => log.warn(msg, { err }));
const timers = [
	setInterval(() => void collab.reauthorizeAll().catch((err) => log.error('re-authorization sweep failed', { err })), config.reauthIntervalMs)
];
if (config.catchUpIntervalMs > 0) timers.push(setInterval(() => collab.catchUpAll(), config.catchUpIntervalMs));

const server = new Server({
	name: 'glyph-collab',
	port: config.port,
	quiet: true,
	debounce: config.storeDebounceMs,
	maxDebounce: config.storeMaxDebounceMs,
	// Persist and drop a document as soon as its last editor leaves.
	unloadImmediately: true,
	timeout: 30000,
	extensions: [
		collab,
		http,
		{
			async onDestroy() {
				for (const t of timers) clearInterval(t);
				await stopListening();
				await persistence.close();
				log.info('collab service stopped');
			}
		}
	]
});

await server.listen();
log.info('collab service listening', { port: config.port, schemaFingerprint: fingerprint });
