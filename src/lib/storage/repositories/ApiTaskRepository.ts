import { api } from '$lib/storage/apiClient';
import type { FilterContext } from '$lib/storage/filterUtils';
import type { Task, FilterSet } from '$lib/models/types';

/**
 * API-backed task repository. Mirrors TaskRepository's public interface.
 */
export class ApiTaskRepository {
	async getAll(): Promise<Task[]> {
		return (await api.get<Task[]>('/api/v1/tasks')) ?? [];
	}

	async getById(id: string): Promise<Task | null> {
		return api.getOrNull<Task>(`/api/v1/tasks/${id}`);
	}

	async create(item: Task): Promise<Task> {
		return api.post<Task>('/api/v1/tasks', item);
	}

	async update(id: string, patch: Partial<Omit<Task, 'id'>>): Promise<Task | null> {
		return api.patch<Task>(`/api/v1/tasks/${id}`, patch);
	}

	async delete(id: string): Promise<boolean> {
		await api.del(`/api/v1/tasks/${id}`);
		return true;
	}

	async upsert(item: Task): Promise<Task> {
		return api.put<Task>(`/api/v1/tasks/${item.id}`, item);
	}

	async getByPageId(pageId: string): Promise<Task[]> {
		return (await api.get<Task[]>(`/api/v1/tasks?sourcePageId=${encodeURIComponent(pageId)}`)) ?? [];
	}

	async getByNodeId(nodeId: string): Promise<Task | null> {
		const tasks = await api.get<Task[]>(`/api/v1/tasks?sourceNodeId=${encodeURIComponent(nodeId)}`);
		return (tasks ?? [])[0] ?? null;
	}

	// The server resolves synthetic fields (e.g. sourcePageTags) via SQL, so the
	// client-side FilterContext is accepted for interface parity but unused here.
	applyFilter(tasks: Task[], filterSet: FilterSet, _ctx?: FilterContext): Task[] | Promise<Task[]> {
		if (filterSet.rules.length === 0) return tasks;
		return api.post<Task[]>('/api/v1/tasks/filter', filterSet);
	}
}
