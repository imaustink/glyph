/**
 * How a note's content maps to tasks: which bullets are TODO bullets, and
 * how a task's status shows on its bullet.
 *
 * The Go API does the same derivation for content written outside the
 * editor (api/internal/pmmd/todo.go, used by MCP). Both are tested against
 * one shared fixture file, api/internal/pmmd/testdata/todo_derivation.json,
 * so they can't drift apart. Change them together.
 *
 * Keep this module free of DOM, Svelte and store imports.
 */
import type { TaskStatus, TodoTriggerConfig } from '$lib/models/types';
import { DEFAULT_TODO_TRIGGER } from '$lib/models/types';
import { safeRegexTest } from '$lib/utils/safeRegex';

/** A bullet shows as ticked when its task is finished either way. */
export function isCheckedStatus(status: TaskStatus | string): boolean {
	return status === 'done' || status === 'cancelled';
}

export interface ResolvedTrigger {
	pattern: string;
	matchMode: 'exact' | 'regex';
	blockTypes: string[];
}

/**
 * The trigger actually in effect. No trigger, or one with an empty pattern,
 * means the default: a block whose text is "TODO" (exact). An empty pattern
 * keeps the configured block types. (A stored `{pattern: ''}` used to mean
 * "match nothing" here but "TODO" in Go — see DI-07/DI-27.)
 */
export function resolveTrigger(config: TodoTriggerConfig | null | undefined): ResolvedTrigger {
	const blockTypes = config?.blockTypes?.length ? [...config.blockTypes] : ['heading'];
	if (!config || !config.pattern?.trim()) {
		return { pattern: DEFAULT_TODO_TRIGGER.pattern, matchMode: 'exact', blockTypes };
	}
	return { pattern: config.pattern, matchMode: config.matchMode === 'regex' ? 'regex' : 'exact', blockTypes };
}

/** List nodes are never trigger blocks, not even with the 'any' block type. */
const LIST_TYPES = new Set(['bulletList', 'orderedList']);

/** Whether a top-level block of this type opens (or closes) a TODO section. */
export function isTriggerBlock(typeName: string, trigger: ResolvedTrigger): boolean {
	if (LIST_TYPES.has(typeName)) return trigger.blockTypes.includes(typeName);
	return trigger.blockTypes.includes('any') || trigger.blockTypes.includes(typeName);
}

/**
 * Whether a regex uses a construct only JavaScript supports (Go's RE2 can't
 * compile it): lookaround, backreferences, `\u` / `\c` escapes. Such a
 * pattern would detect tasks in the editor but not in Go, so both refuse it.
 * (Go refuses the RE2-only constructs; see pmmd/todo.go.)
 */
export function usesJsOnlyRegexSyntax(pattern: string): boolean {
	for (let i = 0; i < pattern.length; i++) {
		const ch = pattern[i];
		if (ch === '\\') {
			const next = pattern[i + 1] ?? '';
			if (/[1-9ukc]/.test(next)) return true;
			i++;
			continue;
		}
		if (ch === '(' && pattern[i + 1] === '?') {
			const rest = pattern.slice(i + 2, i + 4);
			if (rest.startsWith('=') || rest.startsWith('!') || rest === '<=' || rest === '<!') return true;
		}
	}
	return false;
}

/** Whether a trigger block's (trimmed) text opens a TODO section. */
export function matchesTrigger(text: string, trigger: ResolvedTrigger): boolean {
	if (trigger.matchMode === 'regex') {
		if (usesJsOnlyRegexSyntax(trigger.pattern)) return false;
		return safeRegexTest(trigger.pattern, text);
	}
	return text.toLowerCase() === trigger.pattern.trim().toLowerCase();
}

/**
 * Whether a detected TODO bullet should become a task. An empty bullet only
 * does while the user is typing in it — the "smart bullet" that turns into a
 * task as soon as it's made. Empty bullets that merely exist in the content
 * never do; Go (MCP) skips them too.
 */
export function shouldCreateTaskFor(bullet: { bulletText: string; hasCursor?: boolean }): boolean {
	return bullet.bulletText.trim() !== '' || bullet.hasCursor === true;
}
