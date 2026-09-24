// Bundles the collab service (including the editor schema it shares with the
// frontend from ../src/lib) into a single dist/server.js.
import * as esbuild from 'esbuild';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const watch = process.argv.includes('--watch');

const options = {
	entryPoints: [path.join(here, 'src/server.ts')],
	outfile: path.join(here, 'dist/server.js'),
	bundle: true,
	platform: 'node',
	target: 'node22',
	format: 'esm',
	sourcemap: true,
	tsconfig: path.join(here, 'tsconfig.json'),
	// pg optionally requires its native binding; we use the pure-JS client.
	external: ['pg-native'],
	// Some bundled CommonJS dependencies call require(); give ESM output one.
	banner: {
		js: "import { createRequire as __glyphCreateRequire } from 'node:module'; const require = __glyphCreateRequire(import.meta.url);"
	},
	logLevel: 'info'
};

if (watch) {
	const ctx = await esbuild.context(options);
	await ctx.watch();
} else {
	await esbuild.build(options);
}
