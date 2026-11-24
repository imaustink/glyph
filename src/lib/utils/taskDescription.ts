/**
 * Whether a task's markdown description has any real content (#109).
 *
 * A cleared description editor can still leave whitespace or `&nbsp;` behind,
 * so those don't count as content.
 */
export function hasDescription(description: string | null | undefined): boolean {
  if (!description) return false;
  return description.replace(/&nbsp;| /g, '').trim().length > 0;
}
