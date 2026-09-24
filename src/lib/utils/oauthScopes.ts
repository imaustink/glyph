import type { OAuthScope } from '$lib/models/types';

const SCOPE_NOUNS: Record<string, string> = {
	page: 'pages',
	task: 'tasks',
	template: 'templates',
	lane: 'board lanes',
	org: 'organization info'
};

/** Human-readable label for an OAuth scope, e.g. `task:write` → "Edit tasks". */
export function scopeReadable(scope: OAuthScope): string {
	const [resource, perm] = scope.split(':');
	const noun = SCOPE_NOUNS[resource] ?? resource;
	return perm === 'write' ? `Edit ${noun}` : `View ${noun}`;
}
