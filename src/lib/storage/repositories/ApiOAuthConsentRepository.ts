import { api } from '$lib/storage/apiClient';
import type { OAuthConsentInfo } from '$lib/models/types';

export class ApiOAuthConsentRepository {
	async getInfo(query: string): Promise<OAuthConsentInfo> {
		return api.get<OAuthConsentInfo>(`/api/v1/oauth/consent?${query}`);
	}

	async decide(consentToken: string, approve: boolean): Promise<{ redirectUrl: string }> {
		return api.post<{ redirectUrl: string }>('/api/v1/oauth/consent/decision', {
			consentToken,
			approve
		});
	}
}

export const oauthConsentRepository = new ApiOAuthConsentRepository();
