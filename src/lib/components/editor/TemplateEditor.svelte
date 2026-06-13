<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Editor } from '@tiptap/core';
  import StarterKit from '@tiptap/starter-kit';
  import Placeholder from '@tiptap/extension-placeholder';
  import { TaskLinkExtension } from '$lib/editor/extensions/TaskLinkExtension';

  let {
    content,
    onchange
  }: {
    content: string;
    onchange: (json: string) => void;
  } = $props();

  let editorEl = $state<HTMLDivElement | null>(null);
  let editor = $state<Editor | null>(null);

  onMount(() => {
    if (!editorEl) return;

    let initialContent: object | string = '';
    if (content) {
      try {
        initialContent = JSON.parse(content);
      } catch {
        initialContent = content;
      }
    }

    editor = new Editor({
      element: editorEl,
      extensions: [
        StarterKit.configure({ listItem: false }),
        TaskLinkExtension,
        Placeholder.configure({
          placeholder: 'Write your template content here…'
        })
      ],
      content: initialContent || '',
      editorProps: {
        attributes: {
          class: 'tiptap-editor-template',
          spellcheck: 'true'
        }
      },
      onUpdate: ({ editor: ed }) => {
        onchange(JSON.stringify(ed.getJSON()));
      }
    });
  });

  onDestroy(() => {
    editor?.destroy();
  });
</script>

<div bind:this={editorEl} class="template-editor-mount"></div>

<style>
  .template-editor-mount {
    min-height: 180px;
    border: 1px solid var(--border-default);
    border-radius: var(--radius-md);
    padding: 12px 16px;
    cursor: text;
    background: var(--bg-primary);
  }

  .template-editor-mount:focus-within {
    border-color: var(--accent);
  }

  :global(.template-editor-mount .tiptap-editor-template) {
    outline: none;
    min-height: 140px;
    color: var(--text-primary);
    font-size: var(--font-size-sm);
    line-height: 1.7;
    caret-color: var(--accent);
  }

  :global(.template-editor-mount .tiptap-editor-template > :first-child) {
    margin-top: 0 !important;
  }

  :global(.template-editor-mount .tiptap-editor-template h1) { font-size: 1.4em; font-weight: 700; color: var(--text-heading); margin: 0.8em 0 0.3em; }
  :global(.template-editor-mount .tiptap-editor-template h2) { font-size: 1.15em; font-weight: 600; color: var(--text-heading); margin: 0.7em 0 0.25em; }
  :global(.template-editor-mount .tiptap-editor-template h3) { font-size: 1em; font-weight: 600; color: var(--text-heading); margin: 0.6em 0 0.2em; }
  :global(.template-editor-mount .tiptap-editor-template p) { margin: 0.3em 0; }
  :global(.template-editor-mount .tiptap-editor-template ul),
  :global(.template-editor-mount .tiptap-editor-template ol) { padding-left: 1.4em; margin: 0.3em 0; }

  :global(.template-editor-mount .tiptap-editor-template p.is-editor-empty:first-child::before) {
    color: var(--text-muted);
    content: attr(data-placeholder);
    float: left;
    height: 0;
    pointer-events: none;
  }
</style>
