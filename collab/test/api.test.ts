/**
 * HttpApi's mapping of the API's snapshot answers onto what the extension
 * does next (evict, quarantine, catch up …), against a stub HTTP server.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import http from 'node:http';
import type { AddressInfo } from 'node:net';
import { HttpApi } from '../src/api.js';

let answer: { status: number; body: unknown } = { status: 200, body: { revision: 1 } };
let server: http.Server;
let api: HttpApi;

beforeAll(async () => {
	server = http.createServer((req, res) => {
		req.resume();
		req.on('end', () => {
			res.writeHead(answer.status, { 'content-type': 'application/json' });
			res.end(JSON.stringify(answer.body));
		});
	});
	await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
	api = new HttpApi(`http://127.0.0.1:${(server.address() as AddressInfo).port}`, 'token');
});
afterAll(() => new Promise<void>((r) => server.close(() => r())));

const body = { epoch: 1, upToSeq: 5, content: { type: 'doc' }, schemaVersion: 1 };
const PAGE = '11111111-1111-4111-8111-111111111111';

describe('HttpApi.snapshot', () => {
	it('maps a snapshot that is merely behind a newer one to "behind", not "stale" [DI-11]', async () => {
		// Another replica's snapshot for a later seq landed first. The document
		// was not replaced, so this must not evict (which drops pending edits).
		answer = { status: 409, body: { code: 'snapshot_behind', error: 'seq 5 is behind 6' } };
		expect(await api.snapshot(PAGE, body)).toEqual({ kind: 'behind' });
	});

	it('still maps a replaced epoch to "stale"', async () => {
		answer = { status: 409, body: { code: 'stale_snapshot', error: 'epoch 1 is not current (2)' } };
		expect(await api.snapshot(PAGE, body)).toEqual({ kind: 'stale' });
	});

	it('passes on that the kill switch is off when a final snapshot is accepted [DI-14]', async () => {
		answer = { status: 200, body: { revision: 8, disabled: true } };
		expect(await api.snapshot(PAGE, body)).toEqual({ kind: 'ok', revision: 8, disabled: true });
	});

	it('maps an accepted snapshot to "ok"', async () => {
		answer = { status: 200, body: { revision: 7 } };
		expect(await api.snapshot(PAGE, body)).toMatchObject({ kind: 'ok', revision: 7 });
	});
});
