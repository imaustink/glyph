// @vitest-environment node
/**
 * Guards the API's content validator against drifting from the editor schema.
 *
 * The Go validator (api/internal/handler/content_validator.go) keeps its own
 * allowlist of node types, mark types and attributes. Content it accepts but
 * the editor can't represent loads as a blank editor (DI-01); attributes the
 * editor has but the validator strips are silently lost on every save
 * (DI-24). Both sides are pinned to one checked-in fixture:
 *
 * - this test asserts the fixture equals the live `documentSchema()`, and
 * - a Go test (TestAllowlistMatchesEditorSchema) asserts the allowlist equals
 *   the fixture.
 *
 * After changing the editor schema, regenerate the fixture with
 * `pnpm schema:fixture`, then update the Go allowlist until its test passes.
 */
import { describe, expect, it } from 'vitest';
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { documentSchema } from '$lib/editor/schema';

const FIXTURE = fileURLToPath(
	new URL('../../../api/internal/handler/testdata/editor_schema.json', import.meta.url)
);

type SchemaDump = {
	nodes: Record<string, string[]>;
	marks: Record<string, string[]>;
};

/** Node/mark names with their attribute names, sorted so the dump is stable. */
function dumpSchema(): SchemaDump {
	const schema = documentSchema();
	const collect = (
		forEach: (fn: (name: string, spec: { attrs?: Record<string, unknown> | null }) => void) => void
	) => {
		const out: Record<string, string[]> = {};
		const names: string[] = [];
		const attrs: Record<string, string[]> = {};
		forEach((name, spec) => {
			names.push(name);
			attrs[name] = Object.keys(spec.attrs ?? {}).sort();
		});
		for (const name of names.sort()) out[name] = attrs[name];
		return out;
	};
	return {
		nodes: collect((fn) => schema.spec.nodes.forEach(fn)),
		marks: collect((fn) => schema.spec.marks.forEach(fn))
	};
}

describe('editor schema fixture', () => {
	it('matches the live editor schema (regenerate with `pnpm schema:fixture`)', () => {
		const live = dumpSchema();
		if (process.env.UPDATE_EDITOR_SCHEMA_FIXTURE === '1') {
			writeFileSync(FIXTURE, JSON.stringify(live, null, '\t') + '\n');
		}
		const fixture = JSON.parse(readFileSync(FIXTURE, 'utf8')) as SchemaDump;
		expect(fixture).toEqual(live);
	});
});
