import { api } from '$lib/storage/apiClient';
import type { NoteTemplate } from '$lib/models/types';

/**
 * API-backed template repository. Mirrors TemplateRepository's public interface.
 */
export class ApiTemplateRepository {
	async getAll(): Promise<NoteTemplate[]> {
		return (await api.get<NoteTemplate[]>('/api/v1/templates')) ?? [];
	}

	async getById(id: string): Promise<NoteTemplate | null> {
		return api.getOrNull<NoteTemplate>(`/api/v1/templates/${id}`);
	}

	async create(item: NoteTemplate): Promise<NoteTemplate> {
		return api.post<NoteTemplate>('/api/v1/templates', item);
	}

	async update(id: string, patch: Partial<Omit<NoteTemplate, 'id'>>): Promise<NoteTemplate | null> {
		return api.patch<NoteTemplate>(`/api/v1/templates/${id}`, patch);
	}

	async delete(id: string): Promise<boolean> {
		await api.del(`/api/v1/templates/${id}`);
		return true;
	}

	async upsert(item: NoteTemplate): Promise<NoteTemplate> {
		return api.put<NoteTemplate>(`/api/v1/templates/${item.id}`, item);
	}
}
