import { api } from '$lib/storage/apiClient';
import type { OAuthConnection } from '$lib/models/types';

/**
 * Apps the current user has connected to their account (OAuth grants).
 * Session-only endpoints — bearer tokens can't list or revoke grants.
 */
export class ApiOAuthConnectionsRepository {
	async list(): Promise<OAuthConnection[]> {
		return (await api.get<OAuthConnection[]>('/api/v1/oauth/connections')) ?? [];
	}

	async revoke(connectionId: string): Promise<void> {
		await api.del(`/api/v1/oauth/connections/${encodeURIComponent(connectionId)}`);
	}
}
