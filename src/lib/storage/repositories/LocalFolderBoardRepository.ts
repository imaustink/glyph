import type { Lane, Task, TreeNode } from '$lib/models/types';
import type { IPageRepository } from '$lib/storage/interfaces';
import type { LaneRepository } from '$lib/storage/repositories/LaneRepository';
import type { TaskRepository } from '$lib/storage/repositories/TaskRepository';
import { uuid } from '$lib/utils/uuid';
import { now } from '$lib/utils/time';

/**
 * LocalStorage-mode folder board repository.
 *
 * Computes folder descendants client-side by walking the page tree, then
 * filters lanes and tasks by folderId / sourcePageId accordingly.
 *
 * In localStorage mode all data is owned by the current user, so canEdit
 * is always true.
 */
export class LocalFolderBoardRepository {
  constructor(
    private pages: IPageRepository,
    private lanes: LaneRepository,
    private tasks: TaskRepository
  ) {}

  async getFolder(folderId: string): Promise<{ folder: TreeNode; canEdit: boolean }> {
    const folder = await this.pages.getById(folderId);
    if (!folder) throw new Error(`Folder not found: ${folderId}`);
    return { folder, canEdit: true };
  }

  async getLanes(folderId: string): Promise<Lane[]> {
    const all = await this.lanes.getAll();
    return all
      .filter((l) => l.folderId === folderId)
      .sort((a, b) => a.order - b.order);
  }

  async createLane(folderId: string, lane: Omit<Lane, 'id' | 'createdAt' | 'updatedAt'>): Promise<Lane> {
    const timestamp = now();
    const newLane: Lane = {
      ...lane,
      id: uuid(),
      folderId,
      createdAt: timestamp,
      updatedAt: timestamp
    };
    return this.lanes.create(newLane);
  }

  async updateLane(folderId: string, laneId: string, patch: Partial<Omit<Lane, 'id'>>): Promise<Lane> {
    const updated = await this.lanes.update(laneId, patch);
    if (!updated) throw new Error(`Lane not found: ${laneId}`);
    return updated;
  }

  async deleteLane(_folderId: string, laneId: string): Promise<void> {
    await this.lanes.delete(laneId);
  }

  async getTasks(folderId: string): Promise<Task[]> {
    const descendantIds = await this._getDescendantIds(folderId);
    const idSet = new Set(descendantIds);
    const all = await this.tasks.getAll();
    return all.filter(
      (t) => (t.sourcePageId && idSet.has(t.sourcePageId)) || t.folderId === folderId
    );
  }

  /** Collects all descendant page IDs (inclusive of the folder itself). */
  private async _getDescendantIds(folderId: string): Promise<string[]> {
    const all = await this.pages.getAll();
    const childrenByParent = new Map<string | null, TreeNode[]>();
    for (const p of all) {
      const list = childrenByParent.get(p.parentId) ?? [];
      list.push(p);
      childrenByParent.set(p.parentId, list);
    }
    const result: string[] = [folderId];
    const queue = [folderId];
    while (queue.length > 0) {
      const current = queue.shift()!;
      const children = childrenByParent.get(current) ?? [];
      for (const child of children) {
        result.push(child.id);
        queue.push(child.id);
      }
    }
    return result;
  }
}
