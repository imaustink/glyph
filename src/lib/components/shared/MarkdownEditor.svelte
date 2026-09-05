<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Editor } from '@tiptap/core';
  import StarterKit from '@tiptap/starter-kit';
  import Placeholder from '@tiptap/extension-placeholder';
  import { Markdown } from '@tiptap/markdown';

  let {
    value = '',
    placeholder = '',
    ariaLabel,
    onchange
  }: {
    value?: string;
    placeholder?: string;
    ariaLabel?: string;
    onchange?: (markdown: string) => void;
  } = $props();

  let editorEl = $state<HTMLDivElement | null>(null);
  let editor = $state<Editor | null>(null);

  onMount(() => {
    if (!editorEl) return;

    editor = new Editor({
      element: editorEl,
      content: value,
      contentType: 'markdown',
      extensions: [
        StarterKit,
        Markdown,
        Placeholder.configure({ placeholder })
      ],
      editorProps: {
        attributes: {
          class: 'md-editor',
          spellcheck: 'true',
          ...(ariaLabel ? { 'aria-label': ariaLabel } : {})
        }
      },
      onUpdate: ({ editor: ed }) => {
        onchange?.(ed.getMarkdown());
      }
    });
  });

  // Sync external value changes (e.g. task switch or store refresh) into the
  // editor without clobbering in-progress edits. Only replaces content when the
  // incoming value differs from what the editor already holds, which prevents a
  // feedback loop with the debounced save round-trip.
  $effect(() => {
    const incoming = value;
    if (!editor) return;
    if (incoming === editor.getMarkdown()) return;
    editor.commands.setContent(incoming, { contentType: 'markdown', emitUpdate: false });
  });

  export function focus() {
    editor?.commands.focus();
  }

  onDestroy(() => {
    editor?.destroy();
  });
</script>

<div bind:this={editorEl} class="md-editor-mount"></div>

<style>
  .md-editor-mount {
    width: 100%;
  }

  :global(.md-editor) {
    min-height: 120px;
    outline: none;
    font-size: var(--font-size-base);
    line-height: 1.6;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
    padding: 12px 14px;
    color: var(--text-primary);
    transition: border-color 0.15s ease;
  }

  :global(.md-editor:focus) {
    border-color: var(--accent);
  }

  :global(.md-editor > :first-child) {
    margin-top: 0 !important;
  }

  :global(.md-editor > :last-child) {
    margin-bottom: 0 !important;
  }

  :global(.md-editor h1) { font-size: 1.5em; font-weight: 700; color: var(--text-heading); margin: 0.8em 0 0.3em; line-height: 1.25; }
  :global(.md-editor h2) { font-size: 1.3em; font-weight: 600; color: var(--text-heading); margin: 0.8em 0 0.3em; }
  :global(.md-editor h3) { font-size: 1.1em; font-weight: 600; color: var(--text-heading); margin: 0.7em 0 0.25em; }
  :global(.md-editor h4) { font-size: 1em; font-weight: 600; color: var(--text-heading); margin: 0.6em 0 0.2em; }

  :global(.md-editor p) { margin: 0.4em 0; }

  :global(.md-editor p.is-editor-empty:first-child::before) {
    content: attr(data-placeholder);
    color: var(--text-muted);
    pointer-events: none;
    float: left;
    height: 0;
  }

  :global(.md-editor ul) { padding-left: 1.5em; margin: 0.3em 0; list-style: disc; }
  :global(.md-editor ol) { padding-left: 1.5em; margin: 0.3em 0; list-style: decimal; }
  :global(.md-editor li) { margin: 0.15em 0; }
  :global(.md-editor li > p) { margin: 0; }

  :global(.md-editor blockquote) {
    border-left: 3px solid var(--border-strong);
    padding-left: 1em;
    margin: 0.5em 0;
    color: var(--text-secondary);
  }

  :global(.md-editor code) {
    background: var(--bg-tertiary);
    border-radius: var(--radius-sm);
    padding: 0.1em 0.35em;
    font-size: 0.9em;
    font-family: var(--font-mono, monospace);
  }

  :global(.md-editor pre) {
    background: var(--bg-tertiary);
    border-radius: var(--radius-md);
    padding: 12px 14px;
    margin: 0.5em 0;
    overflow-x: auto;
  }

  :global(.md-editor pre code) {
    background: transparent;
    padding: 0;
  }

  :global(.md-editor a) {
    color: var(--accent);
    text-decoration: underline;
  }

  :global(.md-editor hr) {
    border: none;
    border-top: 1px solid var(--border-default);
    margin: 0.8em 0;
  }
</style>
