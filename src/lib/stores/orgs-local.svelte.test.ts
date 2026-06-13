/**
 * Tests orgsStore when repo is null (localStorage mode, no API).
 *
 * Separate file required because vi.mock is hoisted per-file.
 */

import { describe, it, expect, vi } from 'vitest';

vi.mock('$lib/storage/config', () => ({
	repositories: { orgs: null },
	storageMode: 'local'
}));

import { orgsStore } from './orgs.svelte';

describe('orgsStore (local mode — repo is null)', () => {
	it('available returns false', () => {
		expect(orgsStore.available).toBe(false);
	});

	it('load() is a no-op (does not throw)', async () => {
		await expect(orgsStore.load()).resolves.toBeUndefined();
	});

	it('createOrg() throws "Orgs not available in local mode"', async () => {
		await expect(orgsStore.createOrg('Test')).rejects.toThrow(/local mode/i);
	});

	it('updateOrg() is a no-op (does not throw)', async () => {
		await expect(orgsStore.updateOrg('org-1', 'New Name')).resolves.toBeUndefined();
	});

	it('deleteOrg() is a no-op (does not throw)', async () => {
		await expect(orgsStore.deleteOrg('org-1')).resolves.toBeUndefined();
	});

	it('getOrgDetail() returns null', async () => {
		expect(await orgsStore.getOrgDetail('org-1')).toBeNull();
	});

	it('addMember() throws "Orgs not available in local mode"', async () => {
		await expect(orgsStore.addMember('org-1', 'a@b.com', 'viewer')).rejects.toThrow(/local mode/i);
	});

	it('updateMemberRole() throws "Orgs not available in local mode"', async () => {
		await expect(orgsStore.updateMemberRole('org-1', 'u1', 'editor')).rejects.toThrow(/local mode/i);
	});

	it('removeMember() is a no-op (does not throw)', async () => {
		await expect(orgsStore.removeMember('org-1', 'u1')).resolves.toBeUndefined();
	});
});
