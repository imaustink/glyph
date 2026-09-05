import { Repository } from '$lib/storage/Repository';
import { applyFilter, type FilterContext } from '$lib/storage/filterUtils';
import type { StorageAdapter, Task, FilterSet } from '$lib/models/types';

export class TaskRepository extends Repository<Task> {
  constructor(adapter: StorageAdapter) {
    super(adapter, 'tasks');
  }

  async getByPageId(pageId: string): Promise<Task[]> {
    return (await this.getAll()).filter((t) => t.sourcePageId === pageId);
  }

  async getByNodeId(nodeId: string): Promise<Task | null> {
    return (await this.getAll()).find((t) => t.sourceNodeId === nodeId) ?? null;
  }

  applyFilter(tasks: Task[], filterSet: FilterSet, ctx?: FilterContext): Task[] {
    return applyFilter(tasks, filterSet, ctx);
  }
}
