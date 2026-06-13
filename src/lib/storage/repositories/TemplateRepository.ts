import { Repository } from '../Repository';
import type { NoteTemplate, StorageAdapter } from '$lib/models/types';

export class TemplateRepository extends Repository<NoteTemplate> {
  constructor(adapter: StorageAdapter) {
    super(adapter, 'templates');
  }
}
