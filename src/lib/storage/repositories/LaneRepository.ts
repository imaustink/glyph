import { Repository } from '$lib/storage/Repository';
import type { StorageAdapter, Lane } from '$lib/models/types';

export class LaneRepository extends Repository<Lane> {
  constructor(adapter: StorageAdapter) {
    super(adapter, 'lanes');
  }

  async getOrdered(): Promise<Lane[]> {
    return (await this.getAll()).sort((a, b) => a.order - b.order);
  }

  /**
   * Reorder lanes in a single batch write instead of N individual updates.
   */
  async reorderAll(orderedIds: string[], updatedAt: string): Promise<void> {
    const patches = new Map<string, Partial<Omit<Lane, 'id'>>>();
    orderedIds.forEach((id, i) => {
      patches.set(id, { order: i, updatedAt });
    });
    await this.updateMany(patches);
  }
}
