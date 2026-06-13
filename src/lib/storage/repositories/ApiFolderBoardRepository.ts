import { api } from '$lib/storage/apiClient';
import type { Lane, Task, TreeNode } from '$lib/models/types';

/**
 * API-backed folder board repository.
 * Wraps the /api/v1/folders/:id/* endpoints.
 */
export class ApiFolderBoardRepository {
  async getFolder(folderId: string): Promise<{ folder: TreeNode; canEdit: boolean }> {
    return api.get<{ folder: TreeNode; canEdit: boolean }>(`/api/v1/folders/${folderId}`);
  }

  async getLanes(folderId: string): Promise<Lane[]> {
    return (await api.get<Lane[]>(`/api/v1/folders/${folderId}/lanes`)) ?? [];
  }

  async createLane(folderId: string, lane: Omit<Lane, 'id' | 'createdAt' | 'updatedAt'>): Promise<Lane> {
    return api.post<Lane>(`/api/v1/folders/${folderId}/lanes`, lane);
  }

  async updateLane(folderId: string, laneId: string, lane: Partial<Omit<Lane, 'id'>>): Promise<Lane> {
    return api.put<Lane>(`/api/v1/folders/${folderId}/lanes/${laneId}`, lane);
  }

  async deleteLane(folderId: string, laneId: string): Promise<void> {
    await api.del(`/api/v1/folders/${folderId}/lanes/${laneId}`);
  }

  async getTasks(folderId: string): Promise<Task[]> {
    return (await api.get<Task[]>(`/api/v1/folders/${folderId}/tasks`)) ?? [];
  }
}
