import { api } from '$lib/storage/apiClient';
import type { TreeNode, PageContent } from '$lib/models/types';
import type { DeleteSubtreeOptions, PageContentMatch, WriteOptions } from '$lib/storage/interfaces';

interface ApiSearchResult {
	id: string;
	type: string;
	title: string;
	snippet: string;
}

/**
 * API-backed page repository. Mirrors the interface of PageRepository
 * but delegates to the Go REST API instead of localStorage.
 */
export class ApiPageRepository {
	async getAll(): Promise<TreeNode[]> {
		return (await api.get<TreeNode[]>('/api/v1/pages')) ?? [];
	}

	async getById(id: string): Promise<TreeNode | null> {
		return api.getOrNull<TreeNode>(`/api/v1/pages/${id}`);
	}

	async create(item: TreeNode): Promise<TreeNode> {
		return api.post<TreeNode>('/api/v1/pages', item);
	}

	async update(id: string, patch: Partial<Omit<TreeNode, 'id'>>): Promise<TreeNode | null> {
		return api.patch<TreeNode>(`/api/v1/pages/${id}`, patch);
	}

	async delete(id: string): Promise<boolean> {
		await api.del(`/api/v1/pages/${id}`);
		return true;
	}

	async upsert(item: TreeNode): Promise<TreeNode> {
		return api.put<TreeNode>(`/api/v1/pages/${item.id}`, item);
	}

	async getContent(pageId: string): Promise<PageContent | null> {
		return api.getOrNull<PageContent>(`/api/v1/pages/${pageId}/content`);
	}

	async saveContent(content: PageContent, opts?: WriteOptions): Promise<PageContent> {
		const path = `/api/v1/pages/${content.pageId}/content`;
		return opts ? api.put<PageContent>(path, content, opts) : api.put<PageContent>(path, content);
	}

	async deleteContent(_pageId: string): Promise<void> {
		// Content is cascade-deleted with the page on the server
	}

	/**
	 * Server-side search (GET /search, pages only), so the client doesn't
	 * have to download every page's content. The server also returns title
	 * matches (with no snippet); only content matches are kept here.
	 */
	async searchContent(query: string, limit: number): Promise<PageContentMatch[]> {
		const q = query.trim();
		if (!q || limit < 1) return [];
		const params = new URLSearchParams({ q, types: 'page', limit: String(limit) });
		const results = (await api.get<ApiSearchResult[]>(`/api/v1/search?${params}`)) ?? [];
		return results
			.filter((r) => r.type === 'page' && r.snippet)
			.map((r) => ({ pageId: r.id, snippet: r.snippet }));
	}

	async deleteWithContent(id: string): Promise<boolean> {
		return this.delete(id);
	}

	/**
	 * Delete a subtree (root + all descendants).
	 * The API backend cascades the delete at the DB level, so only one request
	 * is needed. The descendantIds parameter is accepted for interface
	 * compatibility with the localStorage implementation but is unused.
	 *
	 * The server soft-deletes the subtree's tasks with it unless `keepTasks`
	 * asks it to detach them into standalone tasks instead.
	 */
	async deleteSubtree(id: string, _descendantIds: string[], opts?: DeleteSubtreeOptions): Promise<void> {
		if (opts?.keepTasks) {
			await api.del(`/api/v1/pages/${id}?keepTasks=true`);
			return;
		}
		await this.delete(id);
	}

	getTree(nodes: TreeNode[]): TreeNode[] {
		return nodes
			.filter((n) => n.parentId === null)
			.sort((a, b) => a.order - b.order);
	}

	getChildren(nodes: TreeNode[], parentId: string): TreeNode[] {
		return nodes
			.filter((n) => n.parentId === parentId)
			.sort((a, b) => a.order - b.order);
	}
}
