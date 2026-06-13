/**
 * Store context — enables dependency injection for testing.
 *
 * In production, components import stores directly from their module files
 * (e.g., `import { pagesStore } from '$lib/stores/pages.svelte'`).
 *
 * For tests, use `createTestStores()` to create isolated store instances
 * with mock repositories, then provide them via Svelte context.
 *
 * Usage in tests:
 * ```ts
 * import { createPagesStore } from '$lib/stores/pages.svelte';
 * import { createTasksStore } from '$lib/stores/tasks.svelte';
 *
 * const stores = {
 *   pages: createPagesStore(mockPageRepo),
 *   tasks: createTasksStore(mockTaskRepo),
 *   lanes: createLanesStore(mockLaneRepo),
 *   templates: createTemplatesStore(mockTemplateRepo),
 * };
 * ```
 */

export type { IPageRepository, ITaskRepository, ILaneRepository, ITemplateRepository } from '$lib/storage/interfaces';
export { createPagesStore } from '$lib/stores/pages.svelte';
export { createTasksStore } from '$lib/stores/tasks.svelte';
export { createLanesStore } from '$lib/stores/lanes.svelte';
export { createTemplatesStore } from '$lib/stores/templates.svelte';
