import { api } from '$lib/storage/apiClient';
import type { Share, SharePermission, ShareResourceType, UserSearchResult } from '$lib/models/types';

export class ApiShareRepository {
	async list(resourceType: ShareResourceType, resourceId: string): Promise<Share[]> {
		return (
			(await api.get<Share[]>(
				`/api/v1/shares?resourceType=${resourceType}&resourceId=${resourceId}`
			)) ?? []
		);
	}

	async create(
		resourceType: ShareResourceType,
		resourceId: string,
		sharedWithEmail: string,
		permission: SharePermission
	): Promise<Share> {
		return api.post<Share>('/api/v1/shares', {
			resourceType,
			resourceId,
			sharedWithEmail,
			permission
		});
	}

	async updatePermission(shareId: string, permission: SharePermission): Promise<Share> {
		return api.patch<Share>(`/api/v1/shares/${shareId}`, { permission });
	}

	async delete(shareId: string): Promise<void> {
		await api.del(`/api/v1/shares/${shareId}`);
	}

	async searchUsers(query: string): Promise<UserSearchResult[]> {
		if (!query.trim()) return [];
		return (
			(await api.get<UserSearchResult[]>(
				`/api/v1/users/search?q=${encodeURIComponent(query)}`
			)) ?? []
		);
	}
}
