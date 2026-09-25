import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';

export default defineConfig({
	resolve: {
		alias: {
			$lib: fileURLToPath(new URL('../src/lib', import.meta.url))
		}
	},
	test: {
		environment: 'node',
		include: ['test/**/*.test.ts'],
		testTimeout: 15000,
		// Each file starts its own servers; keep them from competing for ports.
		fileParallelism: false
	}
});
