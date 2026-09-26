import { Repository } from '../Repository';
import type { NoteTemplate, StorageAdapter } from '$lib/models/types';

export class TemplateRepository extends Repository<NoteTemplate> {
  constructor(adapter: StorageAdapter) {
    super(adapter, 'templates');
  }

  /** First-run seeding: create `templates` only if storage holds none yet. */
  async seedIfEmpty(templates: NoteTemplate[]): Promise<NoteTemplate[]> {
    return this.seedCollectionIfEmpty(templates);
  }
}
