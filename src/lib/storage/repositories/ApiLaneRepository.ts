import { api } from '$lib/storage/apiClient';
import type { Lane } from '$lib/models/types';

/**
 * API-backed lane repository. Mirrors LaneRepository's public interface.
 */
export class ApiLaneRepository {
	async getAll(): Promise<Lane[]> {
		return (await api.get<Lane[]>('/api/v1/lanes')) ?? [];
	}

	async getById(id: string): Promise<Lane | null> {
		return api.getOrNull<Lane>(`/api/v1/lanes/${id}`);
	}

	async create(item: Lane): Promise<Lane> {
		return api.post<Lane>('/api/v1/lanes', item);
	}

	async createBatch(items: Lane[]): Promise<Lane[]> {
		return api.post<Lane[]>('/api/v1/lanes/batch', items);
	}

	async update(id: string, patch: Partial<Omit<Lane, 'id'>>): Promise<Lane | null> {
		return api.patch<Lane>(`/api/v1/lanes/${id}`, patch);
	}

	async delete(id: string): Promise<boolean> {
		await api.del(`/api/v1/lanes/${id}`);
		return true;
	}

	async upsert(item: Lane): Promise<Lane> {
		return api.put<Lane>(`/api/v1/lanes/${item.id}`, item);
	}

	async getOrdered(): Promise<Lane[]> {
		return (await this.getAll()).sort((a, b) => a.order - b.order);
	}

	async reorderAll(orderedIds: string[], _updatedAt: string): Promise<void> {
		await api.put('/api/v1/lanes/reorder', orderedIds.map((id, i) => ({ id, order: i })));
	}
}
