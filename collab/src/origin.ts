/**
 * Whether a request's Origin may act with the user's cookie. The WebSocket
 * and the ops endpoint both authenticate by the browser's session cookie, so
 * without this any site the user visits could act as them.
 *
 * With an explicit allow-list, the Origin must be on it. Otherwise it must be
 * same-origin: the Origin's host equals the (forwarded) Host.
 */
export function originAllowed(headers: Headers, allowedOrigins: string[]): boolean {
	const origin = headers.get('origin');
	if (!origin) return false;
	const normalised = origin.replace(/\/+$/, '');
	if (allowedOrigins.length > 0) return allowedOrigins.includes(normalised);
	const host = headers.get('x-forwarded-host') ?? headers.get('host');
	if (!host) return false;
	try {
		return new URL(normalised).host === host.split(',')[0].trim();
	} catch {
		return false;
	}
}

/** Node IncomingMessage headers → WHATWG Headers. */
export function toHeaders(raw: Record<string, string | string[] | undefined>): Headers {
	const h = new Headers();
	for (const [k, v] of Object.entries(raw)) {
		if (v === undefined) continue;
		h.set(k, Array.isArray(v) ? v.join(', ') : v);
	}
	return h;
}
