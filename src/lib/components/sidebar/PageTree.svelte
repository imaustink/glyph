<script lang="ts">
  import type { TreeNode } from '$lib/models/types';
  import TreeNodeItem from './TreeNodeItem.svelte';

  let {
    childrenByParent,
    parentId
  }: {
    childrenByParent: Map<string | null, TreeNode[]>;
    parentId: string | null;
  } = $props();

  const children = $derived(childrenByParent.get(parentId) ?? []);
</script>

{#each children as node (node.id)}
  <TreeNodeItem {node} {childrenByParent} />
{/each}
