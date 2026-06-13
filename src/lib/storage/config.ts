/**
 * Storage configuration — determines whether the app uses localStorage or the
 * Go REST API for persistence.
 *
 * Set the environment variable VITE_STORAGE_MODE to control:
 *   "local"  → localStorage (default, no backend needed)
 *   "api"    → REST API at VITE_API_URL (requires running Go API)
 *
 * All repositories are instantiated once and shared across stores.
 */

import { LocalStorageAdapter } from '$lib/storage/LocalStorageAdapter';
import { PageRepository } from '$lib/storage/repositories/PageRepository';
import { TaskRepository } from '$lib/storage/repositories/TaskRepository';
import { LaneRepository } from '$lib/storage/repositories/LaneRepository';
import { TemplateRepository } from '$lib/storage/repositories/TemplateRepository';
import { ApiPageRepository } from '$lib/storage/repositories/ApiPageRepository';
import { ApiTaskRepository } from '$lib/storage/repositories/ApiTaskRepository';
import { ApiLaneRepository } from '$lib/storage/repositories/ApiLaneRepository';
import { ApiTemplateRepository } from '$lib/storage/repositories/ApiTemplateRepository';
import { ApiOrgRepository } from '$lib/storage/repositories/ApiOrgRepository';
import { ApiShareRepository } from '$lib/storage/repositories/ApiShareRepository';
import { ApiFolderBoardRepository } from '$lib/storage/repositories/ApiFolderBoardRepository';
import { LocalFolderBoardRepository } from '$lib/storage/repositories/LocalFolderBoardRepository';
import type { IPageRepository, ITaskRepository, ILaneRepository, ITemplateRepository } from '$lib/storage/interfaces';

export type StorageMode = 'local' | 'api';

export const storageMode: StorageMode =
	(import.meta.env.VITE_STORAGE_MODE as StorageMode) || 'local';

// ─── Shared repository type aliases ───────────────────────────────────────────
// Stores import these interface types so they never couple to concrete classes.

export type PageRepo = IPageRepository;
export type TaskRepo = ITaskRepository;
export type LaneRepo = ILaneRepository;
export type TemplateRepo = ITemplateRepository;
export type OrgRepo = ApiOrgRepository | null;
export type ShareRepo = ApiShareRepository | null;
export type FolderBoardRepo = ApiFolderBoardRepository | LocalFolderBoardRepository;

// ─── Instantiate once ─────────────────────────────────────────────────────────

function createRepositories() {
	if (storageMode === 'api') {
		return {
			pages: new ApiPageRepository() as PageRepo,
			tasks: new ApiTaskRepository() as TaskRepo,
			lanes: new ApiLaneRepository() as LaneRepo,
			templates: new ApiTemplateRepository() as TemplateRepo,
			orgs: new ApiOrgRepository() as OrgRepo,
			shares: new ApiShareRepository() as ShareRepo,
			folderBoard: new ApiFolderBoardRepository() as FolderBoardRepo
		};
	}

	const adapter = new LocalStorageAdapter();
	const pages = new PageRepository(adapter);
	const tasks = new TaskRepository(adapter);
	const lanes = new LaneRepository(adapter);
	return {
		pages: pages as PageRepo,
		tasks: tasks as TaskRepo,
		lanes: lanes as LaneRepo,
		templates: new TemplateRepository(adapter) as TemplateRepo,
		orgs: null as OrgRepo,
		shares: null as ShareRepo,
		folderBoard: new LocalFolderBoardRepository(pages, lanes, tasks) as FolderBoardRepo
	};
}

export const repositories = createRepositories();
