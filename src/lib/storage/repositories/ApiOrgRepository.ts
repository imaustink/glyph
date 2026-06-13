import { api } from '$lib/storage/apiClient';
import type { OrgWithRole, OrgMember, OrgRole, Organization } from '$lib/models/types';

export class ApiOrgRepository {
	async list(): Promise<OrgWithRole[]> {
		return (await api.get<OrgWithRole[]>('/api/v1/orgs')) ?? [];
	}

	async create(name: string): Promise<OrgWithRole> {
		return api.post<OrgWithRole>('/api/v1/orgs', { name });
	}

	async get(id: string): Promise<{ org: Organization; members: OrgMember[] } | null> {
		try {
			return await api.get<{ org: Organization; members: OrgMember[] }>(`/api/v1/orgs/${id}`);
		} catch {
			return null;
		}
	}

	async update(id: string, name: string): Promise<Organization> {
		return api.patch<Organization>(`/api/v1/orgs/${id}`, { name });
	}

	async delete(id: string): Promise<void> {
		await api.del(`/api/v1/orgs/${id}`);
	}

	async addMember(orgId: string, email: string, role: OrgRole): Promise<OrgMember> {
		return api.post<OrgMember>(`/api/v1/orgs/${orgId}/members`, { email, role });
	}

	async updateMemberRole(orgId: string, userId: string, role: OrgRole): Promise<OrgMember> {
		return api.patch<OrgMember>(`/api/v1/orgs/${orgId}/members/${userId}`, { role });
	}

	async removeMember(orgId: string, userId: string): Promise<void> {
		await api.del(`/api/v1/orgs/${orgId}/members/${userId}`);
	}
}
