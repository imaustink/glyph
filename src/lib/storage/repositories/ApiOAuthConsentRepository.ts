import { api } from '$lib/storage/apiClient';
import type { OAuthConsentInfo, OAuthConsentSelection } from '$lib/models/types';

export class ApiOAuthConsentRepository {
	async getInfo(query: string): Promise<OAuthConsentInfo> {
		return api.get<OAuthConsentInfo>(`/api/v1/oauth/consent?${query}`);
	}

	/**
	 * Record the user's decision. `selection` is only sent in selection mode
	 * (dynamic clients), and only when approving — fixed-mode consents carry
	 * their org in the consent token, and a denial grants nothing.
	 */
	async decide(
		consentToken: string,
		approve: boolean,
		selection?: OAuthConsentSelection
	): Promise<{ redirectUrl: string }> {
		return api.post<{ redirectUrl: string }>('/api/v1/oauth/consent/decision', {
			consentToken,
			approve,
			...(approve && selection ? { personal: selection.personal, orgIds: selection.orgIds } : {})
		});
	}
}

export const oauthConsentRepository = new ApiOAuthConsentRepository();
