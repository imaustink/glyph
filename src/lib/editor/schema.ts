/**
 * The page document schema, shared by the browser editor and the collab
 * service.
 *
 * The collab service imports this module (it is bundled from src/lib), so
 * both sides build their ProseMirror schema from literally the same
 * extension list. That matters for data integrity: when y-prosemirror meets a
 * node or mark it can't build with the local schema, it *deletes that content
 * from the shared document*. A client running a different schema would
 * therefore silently erase content for everyone. schemaFingerprint() lets the
 * server refuse such a client before it syncs.
 *
 * Keep this module free of DOM, Svelte and store imports: it must load in
 * Node.
 */
import { getSchema, type AnyExtension } from '@tiptap/core';
import type { Schema } from '@tiptap/pm/model';
import StarterKit from '@tiptap/starter-kit';
import { TaskLinkExtension, type TaskLinkOptions } from '$lib/editor/extensions/TaskLinkExtension';

export { CURRENT_SCHEMA_VERSION } from '$lib/editor/migrations';

/** Name of the Y.XmlFragment that holds the page body in the shared Y.Doc. */
export const COLLAB_FRAGMENT = 'default';

export interface DocumentExtensionOptions {
	/**
	 * Include StarterKit's local undo/redo history. Must be false for
	 * collaborative editing: local history would undo other people's changes.
	 * Yjs' UndoManager (added by the Collaboration extension) replaces it.
	 */
	undoRedo?: boolean;
	taskLink?: Partial<TaskLinkOptions>;
}

/**
 * The schema-defining extensions of the page editor. Editor.svelte adds
 * behaviour-only extensions (placeholder, TODO detection, …) on top; those
 * must not add nodes, marks or attributes, or the fingerprint check below
 * stops covering the real editor schema.
 */
export function documentExtensions(options: DocumentExtensionOptions = {}): AnyExtension[] {
	return [
		StarterKit.configure({
			// Replaced by TaskLinkExtension, which extends ListItem.
			listItem: false,
			...(options.undoRedo === false ? { undoRedo: false as const } : {})
		}),
		options.taskLink ? TaskLinkExtension.configure(options.taskLink) : TaskLinkExtension
	];
}

let cachedSchema: Schema | null = null;

/** The ProseMirror schema for page documents. */
export function documentSchema(): Schema {
	cachedSchema ??= getSchema(documentExtensions());
	return cachedSchema;
}

/**
 * A short, stable fingerprint of everything in a schema that affects how a
 * document is represented: node and mark names, content expressions, groups,
 * and attribute names with their defaults. Two builds with the same
 * fingerprint read and write identical documents.
 */
export function schemaFingerprint(schema: Schema = documentSchema()): string {
	const nodes: unknown[] = [];
	schema.spec.nodes.forEach((name, spec) => {
		nodes.push([
			name,
			spec.content ?? '',
			spec.group ?? '',
			spec.marks ?? null,
			!!spec.inline,
			!!spec.atom,
			attrSignature(spec.attrs)
		]);
	});
	const marks: unknown[] = [];
	schema.spec.marks.forEach((name, spec) => {
		marks.push([name, spec.excludes ?? null, spec.inclusive ?? null, attrSignature(spec.attrs)]);
	});
	return fnv1a64(JSON.stringify([nodes, marks]));
}

function attrSignature(attrs: Record<string, { default?: unknown }> | null | undefined): unknown[] {
	if (!attrs) return [];
	return Object.keys(attrs)
		.sort()
		.map((key) => {
			const def = attrs[key]?.default;
			return [key, typeof def === 'function' ? 'fn' : (def ?? null)];
		});
}

/** 64-bit FNV-1a, hex. Not cryptographic — it only has to detect drift. */
function fnv1a64(input: string): string {
	let hash = 0xcbf29ce484222325n;
	const prime = 0x100000001b3n;
	const mask = 0xffffffffffffffffn;
	for (let i = 0; i < input.length; i++) {
		hash ^= BigInt(input.charCodeAt(i));
		hash = (hash * prime) & mask;
	}
	return hash.toString(16).padStart(16, '0');
}
