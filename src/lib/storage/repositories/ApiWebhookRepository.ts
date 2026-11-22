import { api } from '$lib/storage/apiClient';
import type {
	Webhook,
	WebhookEvent,
	WebhookEventsPage,
	WebhookInput,
	WebhookWithSecret
} from '$lib/models/types';

const base = '/api/v1/webhooks';
const one = (id: string) => `${base}/${encodeURIComponent(id)}`;

/**
 * The current user's webhooks and their event history (last 30 days).
 * Session-only endpoints — bearer tokens can't manage webhooks.
 */
export class ApiWebhookRepository {
	async list(): Promise<Webhook[]> {
		return (await api.get<Webhook[]>(base)) ?? [];
	}

	async get(id: string): Promise<Webhook> {
		return api.get<Webhook>(one(id));
	}

	async create(input: WebhookInput): Promise<WebhookWithSecret> {
		return api.post<WebhookWithSecret>(base, input);
	}

	async update(id: string, patch: Partial<Omit<WebhookInput, 'orgId'>>): Promise<Webhook> {
		return api.patch<Webhook>(one(id), patch);
	}

	async remove(id: string): Promise<void> {
		await api.del(one(id));
	}

	async rotateSecret(id: string): Promise<WebhookWithSecret> {
		return api.post<WebhookWithSecret>(`${one(id)}/rotate-secret`, {});
	}

	async listEvents(id: string, opts: { limit?: number; offset?: number } = {}): Promise<WebhookEventsPage> {
		const params = new URLSearchParams();
		if (opts.limit !== undefined) params.set('limit', String(opts.limit));
		if (opts.offset !== undefined) params.set('offset', String(opts.offset));
		const qs = params.toString();
		const page = await api.get<WebhookEventsPage>(`${one(id)}/events${qs ? `?${qs}` : ''}`);
		return { ...page, events: page?.events ?? [] };
	}

	async replay(id: string, eventId: string): Promise<WebhookEvent> {
		return api.post<WebhookEvent>(`${one(id)}/events/${encodeURIComponent(eventId)}/replay`, {});
	}
}
