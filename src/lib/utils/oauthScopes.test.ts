import { describe, it, expect } from 'vitest';
import { scopeReadable } from './oauthScopes';
import type { OAuthScope } from '$lib/models/types';

describe('scopeReadable', () => {
	it.each<[OAuthScope, string]>([
		['page:read', 'View pages'],
		['page:write', 'Edit pages'],
		['task:read', 'View tasks'],
		['task:write', 'Edit tasks'],
		['template:read', 'View templates'],
		['template:write', 'Edit templates'],
		['lane:read', 'View board lanes'],
		['org:read', 'View organization info']
	])('%s → %s', (scope, label) => {
		expect(scopeReadable(scope)).toBe(label);
	});

	it('falls back to the raw resource name for unknown scopes', () => {
		expect(scopeReadable('widget:read' as OAuthScope)).toBe('View widget');
	});
});
