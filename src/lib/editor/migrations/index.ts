/**
 * ProseMirror document schema migration registry.
 *
 * Each entry in the `migrations` array upgrades documents from a given
 * `fromVersion` to `fromVersion + 1`.
 *
 * HOW TO ADD A MIGRATION:
 *  1. Increment CURRENT_SCHEMA_VERSION.
 *  2. Add an entry to `migrations` with `fromVersion` = the old version.
 *  3. The `migrate` function receives the parsed ProseMirror JSON and returns
 *     a (potentially mutated) copy. Keep it pure — do not mutate in-place.
 *
 * The migration runs automatically on the next document open (read-side
 * migration in PageRepository). No manual intervention is needed.
 */

import type { ProseMirrorJSONNode } from '$lib/models/types';

export const CURRENT_SCHEMA_VERSION = 1;

export interface SchemaMigration {
  fromVersion: number;
  migrate(doc: ProseMirrorJSONNode): ProseMirrorJSONNode;
}

/**
 * Registered migrations in ascending order of fromVersion.
 * Currently empty — no schema changes have required migration since v1.
 */
export const migrations: SchemaMigration[] = [
  // Example (do not remove — shows the pattern):
  // {
  //   fromVersion: 1,
  //   migrate(doc) {
  //     // rename hypothetical node type 'myNode' → 'renamedNode'
  //     return transformNodes(doc, (node) =>
  //       node.type === 'myNode' ? { ...node, type: 'renamedNode' } : node
  //     );
  //   }
  // }
];

/**
 * Apply all pending migrations to bring `doc` from `fromVersion` up to
 * `CURRENT_SCHEMA_VERSION`. Returns the (possibly new) document and the
 * version it was migrated to.
 */
export function applyMigrations(
  doc: ProseMirrorJSONNode,
  fromVersion: number
): { doc: ProseMirrorJSONNode; version: number } {
  let current = doc;
  let version = fromVersion;

  while (version < CURRENT_SCHEMA_VERSION) {
    const migration = migrations.find((m) => m.fromVersion === version);
    if (!migration) break;
    current = migration.migrate(current);
    version = migration.fromVersion + 1;
  }

  return { doc: current, version };
}
