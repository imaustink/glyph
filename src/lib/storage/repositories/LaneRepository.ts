import { Repository } from '$lib/storage/Repository';
import type { StorageAdapter, Lane } from '$lib/models/types';

/** Global-board lanes; folder-board lanes (folderId set) share the same key. */
const isGlobalLane = (lane: Lane) => !lane.folderId;

export class LaneRepository extends Repository<Lane> {
  constructor(adapter: StorageAdapter) {
    super(adapter, 'lanes');
  }

  /**
   * The global board's lanes, in order. Folder-board lanes live under the
   * same storage key but belong to their folder's board only (see
   * LocalFolderBoardRepository), so they are left out.
   */
  async getOrdered(): Promise<Lane[]> {
    return (await this.getAll()).filter(isGlobalLane).sort((a, b) => a.order - b.order);
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

  /** First-run seeding: create `lanes` only if storage holds no global lanes yet. */
  async seedIfEmpty(lanes: Lane[]): Promise<Lane[]> {
    const result = await this.seedCollectionIfEmpty(lanes, isGlobalLane);
    return [...result].sort((a, b) => a.order - b.order);
  }
}
