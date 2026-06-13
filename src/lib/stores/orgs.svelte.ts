import { repositories } from '$lib/storage/config';
import type { OrgWithRole, OrgMember, OrgRole } from '$lib/models/types';
import { handleAuthError } from '$lib/storage/apiClient';

const repo = repositories.orgs;

function createOrgsStore() {
	let orgs = $state<OrgWithRole[]>([]);
	let loaded = $state(false);
	let _loadPromise: Promise<void> | null = null;

	async function load() {
		if (!repo) return;
		if (_loadPromise) return _loadPromise;
		_loadPromise = (async () => {
			try {
				orgs = await repo.list();
				loaded = true;
			} catch (err) {
				handleAuthError(err);
			} finally {
				_loadPromise = null;
			}
		})();
		return _loadPromise;
	}

	async function createOrg(name: string): Promise<OrgWithRole> {
		if (!repo) throw new Error('Orgs not available in local mode');
		const created = await repo.create(name);
		orgs = [...orgs, created];
		return created;
	}

	async function updateOrg(id: string, name: string): Promise<void> {
		if (!repo) return;
		const updated = await repo.update(id, name);
		orgs = orgs.map((o) => (o.id === id ? { ...o, ...updated } : o));
	}

	async function deleteOrg(id: string): Promise<void> {
		if (!repo) return;
		await repo.delete(id);
		orgs = orgs.filter((o) => o.id !== id);
	}

	async function getOrgDetail(id: string): Promise<{ members: OrgMember[] } | null> {
		if (!repo) return null;
		return repo.get(id);
	}

	async function addMember(orgId: string, email: string, role: OrgRole): Promise<OrgMember> {
		if (!repo) throw new Error('Orgs not available in local mode');
		return repo.addMember(orgId, email, role);
	}

	async function updateMemberRole(
		orgId: string,
		userId: string,
		role: OrgRole
	): Promise<OrgMember> {
		if (!repo) throw new Error('Orgs not available in local mode');
		return repo.updateMemberRole(orgId, userId, role);
	}

	async function removeMember(orgId: string, userId: string): Promise<void> {
		if (!repo) return;
		await repo.removeMember(orgId, userId);
	}

	return {
		get orgs() {
			return orgs;
		},
		get loaded() {
			return loaded;
		},
		get available() {
			return repo !== null;
		},
		load,
		createOrg,
		updateOrg,
		deleteOrg,
		getOrgDetail,
		addMember,
		updateMemberRole,
		removeMember
	};
}

export const orgsStore = createOrgsStore();
