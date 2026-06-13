import { api } from '$lib/storage/apiClient';
import type { TreeNode, PageContent } from '$lib/models/types';

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

	async saveContent(content: PageContent): Promise<void> {
		await api.put(`/api/v1/pages/${content.pageId}/content`, content);
	}

	async deleteContent(_pageId: string): Promise<void> {
		// Content is cascade-deleted with the page on the server
	}

	async deleteWithContent(id: string): Promise<boolean> {
		return this.delete(id);
	}

	/**
	 * Delete a subtree (root + all descendants).
	 * The API backend cascades the delete at the DB level, so only one request
	 * is needed. The descendantIds parameter is accepted for interface
	 * compatibility with the localStorage implementation but is unused.
	 */
	async deleteSubtree(id: string, _descendantIds: string[]): Promise<void> {
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
