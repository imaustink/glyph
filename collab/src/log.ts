/** Minimal structured logger (JSON lines on stdout/stderr). */
export interface Logger {
	info(msg: string, fields?: Record<string, unknown>): void;
	warn(msg: string, fields?: Record<string, unknown>): void;
	error(msg: string, fields?: Record<string, unknown>): void;
}

function serialise(fields: Record<string, unknown> | undefined): Record<string, unknown> {
	if (!fields) return {};
	const out: Record<string, unknown> = {};
	for (const [k, v] of Object.entries(fields)) {
		out[k] = v instanceof Error ? { message: v.message, name: v.name } : v;
	}
	return out;
}

export function jsonLogger(): Logger {
	const write = (level: string, stream: NodeJS.WriteStream) => (msg: string, fields?: Record<string, unknown>) =>
		stream.write(JSON.stringify({ time: new Date().toISOString(), level, msg, ...serialise(fields) }) + '\n');
	return { info: write('info', process.stdout), warn: write('warn', process.stderr), error: write('error', process.stderr) };
}

export const silentLogger: Logger = { info() {}, warn() {}, error() {} };
